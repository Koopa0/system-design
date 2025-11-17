# News Feed 系統設計文檔

## 凌晨三點的系統告警

2024 年 12 月 1 日凌晨 3:00

社交平台「TwitterLite」的工程師 Emma 被手機鈴聲吵醒。

**告警訊息**：
```
[CRITICAL] Timeline API P99 latency: 8.5s (threshold: 1s)
[CRITICAL] Database connection pool exhausted: 500/500
[WARNING] User complaints: 2,547 reports in last 5 minutes
```

Emma 跳起來打開筆記本，看到用戶抱怨：

```
@angry_user: 刷新動態要等 10 秒？這什麼破 App！
@frustrated_dev: 我關注了 200 人，每次刷新都像在等世界末日
@impatient_mom: 想看孫子的照片結果 App 卡死了 😡
```

Emma 查看監控：

```
Timeline API 負載：
- QPS: 50,000 req/s
- P50 latency: 3.2s
- P99 latency: 8.5s
- Database queries per request: 平均 150 次 ❌

問題：每個用戶刷新動態，需要查詢所有關注者的帖子，然後合併排序
```

**Emma** 趕緊打電話給資深架構師 David：「David！我們的 Timeline API 快炸了！」

**David**：「別慌，描述一下現在的實現方式。」

**Emma**：「用戶刷新動態時，我們查詢他關注的所有人的最新帖子，然後按時間排序返回。」

**David**：「這是最簡單的 **Pull 模型**（Fanout-on-Read）。讓我們一步步優化。」

---

## 第一幕：Pull 模型的覺醒

第二天上午 10:00，緊急技術會議

**David** 在白板上畫出當前架構：

```
Pull 模型（Fanout-on-Read）

用戶 Alice 刷新動態：
1. 查詢 Alice 關注的人：SELECT followee_id FROM follows WHERE follower_id = 'Alice'
   → 返回：[Bob, Charlie, David, ..., Zoe] (100 人)

2. 查詢每個人的最新帖子：
   SELECT * FROM posts WHERE user_id IN (Bob, Charlie, ..., Zoe)
   ORDER BY created_at DESC LIMIT 10

3. 合併排序後返回

問題：
- 如果 Alice 關注 100 人，每個人有 1000 篇帖子
- 需要掃描 100,000 篇帖子，然後排序
- 每次刷新都要重新計算 ❌
```

### 當前實現（Pull 模型）

```go
// internal/timeline.go (Pull 模型 - 有問題的版本)
package internal

import (
    "context"
    "database/sql"
    "sort"
    "time"
)

type Post struct {
    ID        string
    UserID    string
    Content   string
    CreatedAt time.Time
}

type TimelineService struct {
    db *sql.DB
}

// GetTimeline 獲取用戶動態（Pull 模型）
func (s *TimelineService) GetTimeline(ctx context.Context, userID string, limit int) ([]Post, error) {
    // 1. 查詢用戶關注的所有人
    followees, err := s.getFollowees(ctx, userID)
    if err != nil {
        return nil, err
    }

    // 問題：如果關注 1000 人，這裡就有 1000 個 ID
    if len(followees) == 0 {
        return []Post{}, nil
    }

    // 2. 查詢所有關注者的帖子
    var allPosts []Post
    for _, followeeID := range followees {
        // ❌ 問題：N+1 查詢！每個關注者一次查詢
        posts, err := s.getPostsByUser(ctx, followeeID, 100)
        if err != nil {
            continue // 忽略錯誤繼續
        }
        allPosts = append(allPosts, posts...)
    }

    // 3. 按時間排序
    sort.Slice(allPosts, func(i, j int) bool {
        return allPosts[i].CreatedAt.After(allPosts[j].CreatedAt)
    })

    // 4. 取前 N 條
    if len(allPosts) > limit {
        allPosts = allPosts[:limit]
    }

    return allPosts, nil
}

func (s *TimelineService) getFollowees(ctx context.Context, userID string) ([]string, error) {
    query := "SELECT followee_id FROM follows WHERE follower_id = ?"
    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var followees []string
    for rows.Next() {
        var followeeID string
        if err := rows.Scan(&followeeID); err != nil {
            continue
        }
        followees = append(followees, followeeID)
    }

    return followees, nil
}

func (s *TimelineService) getPostsByUser(ctx context.Context, userID string, limit int) ([]Post, error) {
    query := `
        SELECT id, user_id, content, created_at
        FROM posts
        WHERE user_id = ?
        ORDER BY created_at DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var posts []Post
    for rows.Next() {
        var post Post
        if err := rows.Scan(&post.ID, &post.UserID, &post.Content, &post.CreatedAt); err != nil {
            continue
        }
        posts = append(posts, post)
    }

    return posts, nil
}
```

### 性能測試

```
測試場景：用戶關注 100 人

Pull 模型性能：
- Database queries: 1 (關注列表) + 100 (每個人的帖子) = 101 次查詢 ❌
- 查詢時間：101 × 10ms = 1,010ms = 1 秒
- 排序時間：10,000 個帖子排序 ≈ 50ms
- 總延遲：約 1.05 秒

如果用戶關注 1000 人：
- Database queries: 1,001 次查詢 ❌
- 查詢時間：約 10 秒 ❌❌❌
```

**Emma**：「天啊！怪不得這麼慢！每次刷新都要查詢 100 次數據庫！」

**David**：「這就是 Pull 模型的問題：**讀操作很重**。每次讀取都要實時計算。

有沒有辦法提前計算好，讀取時直接返回？」

**Michael**（後端工程師）：「可以在用戶**發帖時**就推送到所有粉絲的動態流！」

**David**：「沒錯！這就是 **Push 模型**（Fanout-on-Write）。」

---

## 第二幕：Fanout-on-Write 的誕生

**David** 畫出新的架構：

```
Push 模型（Fanout-on-Write）

Bob 發布一篇帖子：
1. 寫入 posts 表：
   INSERT INTO posts (id, user_id, content) VALUES (...)

2. 查詢 Bob 的所有粉絲：
   SELECT follower_id FROM follows WHERE followee_id = 'Bob'
   → 返回：[Alice, Charlie, David, ..., Zoe] (1000 人)

3. Fanout：將這篇帖子推送到每個粉絲的 Feed：
   FOR EACH follower IN [Alice, Charlie, ...]:
       INSERT INTO feed (user_id, post_id, created_at)
       VALUES (follower, 'post_123', NOW())

Alice 刷新動態：
1. 直接查詢她的 Feed：
   SELECT post_id FROM feed
   WHERE user_id = 'Alice'
   ORDER BY created_at DESC LIMIT 10

2. 查詢帖子詳情：
   SELECT * FROM posts WHERE id IN (post_1, post_2, ..., post_10)

優勢：
- 讀操作變快：只需 2 次查詢 ✅
- 已經按時間排序好 ✅
```

### Fanout-on-Write 實現

```go
// internal/fanout.go
package internal

import (
    "context"
    "database/sql"
    "fmt"
    "time"
)

type FanoutService struct {
    db *sql.DB
}

// PublishPost 發布帖子（Fanout-on-Write）
func (s *FanoutService) PublishPost(ctx context.Context, userID, content string) error {
    // 1. 創建帖子
    postID := generateID()
    post := Post{
        ID:        postID,
        UserID:    userID,
        Content:   content,
        CreatedAt: time.Now(),
    }

    if err := s.savePost(ctx, post); err != nil {
        return fmt.Errorf("failed to save post: %w", err)
    }

    // 2. 查詢該用戶的所有粉絲
    followers, err := s.getFollowers(ctx, userID)
    if err != nil {
        return fmt.Errorf("failed to get followers: %w", err)
    }

    // 3. Fanout：推送到每個粉絲的 Feed
    for _, followerID := range followers {
        if err := s.addToFeed(ctx, followerID, postID); err != nil {
            // 記錄錯誤但繼續處理其他粉絲
            fmt.Printf("failed to add to feed for user %s: %v\n", followerID, err)
        }
    }

    return nil
}

func (s *FanoutService) savePost(ctx context.Context, post Post) error {
    query := `
        INSERT INTO posts (id, user_id, content, created_at)
        VALUES (?, ?, ?, ?)
    `
    _, err := s.db.ExecContext(ctx, query, post.ID, post.UserID, post.Content, post.CreatedAt)
    return err
}

func (s *FanoutService) getFollowers(ctx context.Context, userID string) ([]string, error) {
    query := "SELECT follower_id FROM follows WHERE followee_id = ?"
    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var followers []string
    for rows.Next() {
        var followerID string
        if err := rows.Scan(&followerID); err != nil {
            continue
        }
        followers = append(followers, followerID)
    }

    return followers, nil
}

func (s *FanoutService) addToFeed(ctx context.Context, userID, postID string) error {
    query := `
        INSERT INTO feed (user_id, post_id, created_at)
        VALUES (?, ?, ?)
    `
    _, err := s.db.ExecContext(ctx, query, userID, postID, time.Now())
    return err
}

// GetTimeline 獲取用戶動態（從預生成的 Feed 讀取）
func (s *FanoutService) GetTimeline(ctx context.Context, userID string, limit int) ([]Post, error) {
    // 1. 從 Feed 表查詢帖子 ID（已排序）
    query := `
        SELECT post_id FROM feed
        WHERE user_id = ?
        ORDER BY created_at DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var postIDs []string
    for rows.Next() {
        var postID string
        if err := rows.Scan(&postID); err != nil {
            continue
        }
        postIDs = append(postIDs, postID)
    }

    if len(postIDs) == 0 {
        return []Post{}, nil
    }

    // 2. 批量查詢帖子詳情
    posts, err := s.getPostsByIDs(ctx, postIDs)
    if err != nil {
        return nil, err
    }

    return posts, nil
}

func (s *FanoutService) getPostsByIDs(ctx context.Context, postIDs []string) ([]Post, error) {
    // 構建 IN 查詢
    placeholders := make([]string, len(postIDs))
    args := make([]interface{}, len(postIDs))
    for i, id := range postIDs {
        placeholders[i] = "?"
        args[i] = id
    }

    query := fmt.Sprintf(`
        SELECT id, user_id, content, created_at
        FROM posts
        WHERE id IN (%s)
    `, joinStrings(placeholders, ","))

    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var posts []Post
    for rows.Next() {
        var post Post
        if err := rows.Scan(&post.ID, &post.UserID, &post.Content, &post.CreatedAt); err != nil {
            continue
        }
        posts = append(posts, post)
    }

    return posts, nil
}

func joinStrings(strs []string, sep string) string {
    if len(strs) == 0 {
        return ""
    }
    result := strs[0]
    for i := 1; i < len(strs); i++ {
        result += sep + strs[i]
    }
    return result
}

func generateID() string {
    return fmt.Sprintf("post_%d", time.Now().UnixNano())
}
```

### 性能對比

```
測試場景：用戶關注 100 人

Pull 模型（Fanout-on-Read）：
- 讀操作：101 次查詢，延遲 1+ 秒 ❌
- 寫操作：1 次插入，延遲 10ms ✅

Push 模型（Fanout-on-Write）：
- 讀操作：2 次查詢，延遲 20ms ✅（提升 50 倍！）
- 寫操作：1 次插入 + 1000 次 Feed 插入，延遲 1+ 秒 ❌
```

**Emma** 興奮地說：「太棒了！讀取速度提升了 50 倍！」

**Michael**：「但是寫入變慢了。如果一個用戶有 1000 個粉絲，發帖需要插入 1000 次...」

**David**：「沒錯。這就是 **空間換時間**：
- 讀操作變快（預計算）
- 寫操作變慢（Fanout）
- 存儲增加（每個用戶一份 Feed）

但對於社交網絡，這是值得的權衡。因為**讀遠多於寫**（讀寫比例通常是 100:1）。」

**Sarah**（DBA）：「那如果是像 Taylor Swift 這樣的明星，有 100 萬粉絲呢？」

**David** 的表情變得嚴肅：「這就是我們接下來要解決的問題...」

---

## 第三幕：明星賬號的災難

一週後，2024 年 12 月 8 日下午 2:00

TwitterLite 上線了 Fanout-on-Write 模型，系統運行良好。

直到...

**Taylor Swift** 加入了平台，並發布了第一篇帖子：「Hello TwitterLite! 🎵」

瞬間，系統告警爆炸：

```
[CRITICAL] Database write timeout: feed table
[CRITICAL] Message queue backlog: 1,000,000 pending tasks
[ERROR] Fanout task failed: timeout after 60s
```

Emma 查看監控數據：

```
Taylor Swift 的帖子 Fanout 狀態：
- 粉絲數：1,000,000 人
- 需要插入：1,000,000 條 Feed 記錄
- 預計時間：1,000,000 × 1ms = 1,000 秒 = 16.7 分鐘 ❌

問題：
1. 數據庫寫入速度跟不上
2. 其他用戶的帖子被阻塞（等待 Fanout 完成）
3. 用戶要等 17 分鐘才能看到帖子 ❌
```

**Emma** 緊急呼叫 David：「David！Taylor Swift 一篇帖子把系統搞癱了！」

**David**：「這就是 **Fanout-on-Write 的致命弱點**：
- 對於普通用戶（粉絲數 < 1000），Fanout 很快
- 對於明星用戶（粉絲數 > 100萬），Fanout 非常慢

我們需要一個**混合模型**（Hybrid Model）。」

---

## 第四幕：混合模型的誕生

**David** 在白板上畫出混合架構：

```
混合模型（Hybrid Model）

核心思想：
- 普通用戶：Fanout-on-Write（Push 模型）✅
- 明星用戶：Fanout-on-Read（Pull 模型）✅

判斷標準：
if followers_count > THRESHOLD (如 10,000):
    使用 Pull 模型
else:
    使用 Fanout-on-Write

讀取流程：
1. 從 Feed 表讀取（Fanout-on-Write 的結果）
2. 實時查詢關注的明星用戶的最新帖子（Pull）
3. 合併排序後返回
```

### 混合模型實現

```go
// internal/hybrid.go
package internal

import (
    "context"
    "database/sql"
    "sort"
)

const (
    // 粉絲數閾值：超過此數量視為明星用戶
    CELEBRITY_THRESHOLD = 10000

    // Feed 容量限制：每個用戶最多保留多少條
    MAX_FEED_SIZE = 1000
)

type HybridService struct {
    db *sql.DB
}

// PublishPost 發布帖子（混合模型）
func (s *HybridService) PublishPost(ctx context.Context, userID, content string) error {
    // 1. 創建帖子
    postID := generateID()
    post := Post{
        ID:        postID,
        UserID:    userID,
        Content:   content,
        CreatedAt: time.Now(),
    }

    if err := s.savePost(ctx, post); err != nil {
        return fmt.Errorf("failed to save post: %w", err)
    }

    // 2. 檢查用戶是否為明星
    isCelebrity, err := s.isCelebrity(ctx, userID)
    if err != nil {
        return err
    }

    if isCelebrity {
        // 明星用戶：不做 Fanout，讀取時實時拉取 ✅
        fmt.Printf("User %s is celebrity, skip fanout\n", userID)
        return nil
    }

    // 3. 普通用戶：Fanout-on-Write
    followers, err := s.getFollowers(ctx, userID)
    if err != nil {
        return err
    }

    for _, followerID := range followers {
        if err := s.addToFeed(ctx, followerID, postID); err != nil {
            fmt.Printf("failed to add to feed for user %s: %v\n", followerID, err)
        }
    }

    return nil
}

func (s *HybridService) isCelebrity(ctx context.Context, userID string) (bool, error) {
    query := "SELECT COUNT(*) FROM follows WHERE followee_id = ?"
    var count int
    err := s.db.QueryRowContext(ctx, query, userID).Scan(&count)
    if err != nil {
        return false, err
    }

    return count > CELEBRITY_THRESHOLD, nil
}

// GetTimeline 獲取用戶動態（混合模型）
func (s *HybridService) GetTimeline(ctx context.Context, userID string, limit int) ([]Post, error) {
    var allPosts []Post

    // 1. 從 Feed 表讀取（Fanout-on-Write 的結果）
    feedPosts, err := s.getFeedPosts(ctx, userID, limit)
    if err != nil {
        return nil, err
    }
    allPosts = append(allPosts, feedPosts...)

    // 2. 查詢關注的明星用戶
    celebrities, err := s.getCelebrityFollowees(ctx, userID)
    if err != nil {
        return nil, err
    }

    // 3. 實時拉取明星用戶的最新帖子（Pull）
    for _, celebrityID := range celebrities {
        posts, err := s.getRecentPosts(ctx, celebrityID, 10)
        if err != nil {
            continue
        }
        allPosts = append(allPosts, posts...)
    }

    // 4. 合併排序（按時間倒序）
    sort.Slice(allPosts, func(i, j int) bool {
        return allPosts[i].CreatedAt.After(allPosts[j].CreatedAt)
    })

    // 5. 取前 N 條
    if len(allPosts) > limit {
        allPosts = allPosts[:limit]
    }

    return allPosts, nil
}

func (s *HybridService) getFeedPosts(ctx context.Context, userID string, limit int) ([]Post, error) {
    query := `
        SELECT p.id, p.user_id, p.content, p.created_at
        FROM feed f
        JOIN posts p ON f.post_id = p.id
        WHERE f.user_id = ?
        ORDER BY f.created_at DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var posts []Post
    for rows.Next() {
        var post Post
        if err := rows.Scan(&post.ID, &post.UserID, &post.Content, &post.CreatedAt); err != nil {
            continue
        }
        posts = append(posts, post)
    }

    return posts, nil
}

func (s *HybridService) getCelebrityFollowees(ctx context.Context, userID string) ([]string, error) {
    // 查詢用戶關注的明星
    query := `
        SELECT f.followee_id
        FROM follows f
        WHERE f.follower_id = ?
          AND (
              SELECT COUNT(*) FROM follows f2
              WHERE f2.followee_id = f.followee_id
          ) > ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, CELEBRITY_THRESHOLD)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var celebrities []string
    for rows.Next() {
        var celebrityID string
        if err := rows.Scan(&celebrityID); err != nil {
            continue
        }
        celebrities = append(celebrities, celebrityID)
    }

    return celebrities, nil
}

func (s *HybridService) getRecentPosts(ctx context.Context, userID string, limit int) ([]Post, error) {
    query := `
        SELECT id, user_id, content, created_at
        FROM posts
        WHERE user_id = ?
        ORDER BY created_at DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var posts []Post
    for rows.Next() {
        var post Post
        if err := rows.Scan(&post.ID, &post.UserID, &post.Content, &post.CreatedAt); err != nil {
            continue
        }
        posts = append(posts, post)
    }

    return posts, nil
}

// 其他方法（savePost, getFollowers, addToFeed）與之前相同...
```

### 性能對比

```
場景：用戶關注 100 普通用戶 + 5 明星用戶

Pull 模型：
- 讀操作：105 次查詢 ❌

Fanout-on-Write 模型：
- Taylor Swift 發帖：需要 Fanout 給 100 萬粉絲 ❌

混合模型：
- 讀操作：1 次 Feed 查詢 + 5 次明星帖子查詢 = 6 次查詢 ✅
- Taylor Swift 發帖：跳過 Fanout，直接寫入 posts 表 ✅
- 延遲：約 60ms ✅
```

**Emma**：「太聰明了！這樣既保證了讀取速度，又避免了明星帖子的 Fanout 災難！」

**David**：「沒錯。這就是 **Twitter 和 Facebook 的真實做法**：
- 90% 的用戶是普通用戶 → Fanout-on-Write
- 10% 的用戶是明星/大V → Pull

兼顧了性能和成本。」

---

## 第五幕：Feed 排序算法

**Michael**：「現在 Feed 已經很快了，但有個新需求：產品經理希望動態流不只是按時間排序，還要**按相關性排序**。

比如：
- 用戶更關心好友的帖子，而不是陌生人
- 熱門帖子（點讚多、評論多）應該排前面
- 用戶感興趣的話題應該優先顯示

這怎麼實現？」

**David**：「這就是 **Feed 排序算法**（Feed Ranking）。最著名的是 Facebook 的 **EdgeRank 算法**。」

### EdgeRank 算法

```
EdgeRank 公式：

Score = Affinity × Weight × Time_Decay

1. Affinity（親密度）：
   用戶與發帖人的互動頻率
   - 經常點讚/評論 → 高親密度
   - 從不互動 → 低親密度

2. Weight（權重）：
   內容類型的權重
   - 視頻/圖片 > 純文字
   - 被分享的 > 未被分享

3. Time_Decay（時間衰減）：
   - 新帖子 > 舊帖子
   - 指數衰減（exponential decay）
```

### 簡化版排序實現

```go
// internal/ranking.go
package internal

import (
    "math"
    "time"
)

type ScoredPost struct {
    Post  Post
    Score float64
}

type RankingService struct {
    // 用戶親密度數據（實際應從數據庫/緩存讀取）
    affinityCache map[string]map[string]float64
}

// RankPosts 對帖子進行排序
func (s *RankingService) RankPosts(userID string, posts []Post) []Post {
    // 1. 計算每篇帖子的得分
    scoredPosts := make([]ScoredPost, len(posts))
    for i, post := range posts {
        score := s.calculateScore(userID, post)
        scoredPosts[i] = ScoredPost{
            Post:  post,
            Score: score,
        }
    }

    // 2. 按得分排序（降序）
    sort.Slice(scoredPosts, func(i, j int) bool {
        return scoredPosts[i].Score > scoredPosts[j].Score
    })

    // 3. 提取排序後的帖子
    rankedPosts := make([]Post, len(scoredPosts))
    for i, sp := range scoredPosts {
        rankedPosts[i] = sp.Post
    }

    return rankedPosts
}

// calculateScore 計算帖子得分（EdgeRank）
func (s *RankingService) calculateScore(userID string, post Post) float64 {
    affinity := s.getAffinity(userID, post.UserID)
    weight := s.getWeight(post)
    timeDecay := s.getTimeDecay(post.CreatedAt)

    return affinity * weight * timeDecay
}

// getAffinity 獲取親密度（0-1）
func (s *RankingService) getAffinity(userID, authorID string) float64 {
    // 從緩存讀取親密度
    if affinities, ok := s.affinityCache[userID]; ok {
        if affinity, ok := affinities[authorID]; ok {
            return affinity
        }
    }

    // 默認親密度
    return 0.5
}

// getWeight 獲取內容權重
func (s *RankingService) getWeight(post Post) float64 {
    weight := 1.0

    // 根據內容類型調整權重
    if hasImage(post.Content) {
        weight *= 1.5 // 圖片權重高
    }

    if hasVideo(post.Content) {
        weight *= 2.0 // 視頻權重更高
    }

    // 根據互動數據調整權重（簡化版）
    // 實際應從數據庫查詢 likes_count, comments_count
    // weight *= (1 + log(likes_count + 1))

    return weight
}

// getTimeDecay 計算時間衰減（指數衰減）
func (s *RankingService) getTimeDecay(createdAt time.Time) float64 {
    hoursAgo := time.Since(createdAt).Hours()

    // 24 小時衰減到 0.5
    // 48 小時衰減到 0.25
    // 公式：e^(-lambda * t)，lambda = ln(2) / 24
    lambda := math.Log(2) / 24.0
    decay := math.Exp(-lambda * hoursAgo)

    return decay
}

func hasImage(content string) bool {
    // 簡化版：檢查是否包含圖片標記
    return len(content) > 100 // 假設有圖片的帖子內容較長
}

func hasVideo(content string) bool {
    return false // 簡化版
}
```

### 機器學習排序

**David**：「EdgeRank 是基於規則的算法。現代的社交平台（如 Facebook、Instagram）使用**機器學習模型**進行排序：

```
機器學習排序流程：

1. 特徵工程（Feature Engineering）：
   - 用戶特徵：年齡、性別、活躍度
   - 發帖人特徵：粉絲數、發帖頻率
   - 帖子特徵：內容類型、長度、話題標籤
   - 互動特徵：點讚數、評論數、分享數
   - 時間特徵：發帖時間、距離現在的時長

2. 訓練模型：
   - 目標：預測用戶是否會與帖子互動（點讚/評論/分享）
   - 模型：Gradient Boosting（XGBoost/LightGBM）或 Deep Learning
   - 訓練數據：歷史互動數據（數億條樣本）

3. 線上預測：
   - 對 Feed 中的每篇帖子，預測互動概率
   - 按概率排序

4. A/B 測試：
   - 不斷迭代優化模型
   - 目標：提升用戶停留時間、互動率
```

但這超出了本章的範圍。我們先專注於**系統設計**，排序算法可以逐步優化。」

---

## 第六幕：分頁與游標

**Emma**：「現在還有個問題：用戶刷新動態時，我們返回最新的 10 篇帖子。

但用戶往下滑動（Infinite Scroll），需要加載更多帖子。傳統的 OFFSET 分頁會有問題嗎？」

**David**：「很好的問題！傳統的 OFFSET 分頁有嚴重的性能問題和數據一致性問題。」

### OFFSET 分頁的問題

```sql
-- 傳統 OFFSET 分頁
SELECT * FROM feed
WHERE user_id = 'Alice'
ORDER BY created_at DESC
LIMIT 10 OFFSET 0;  -- 第 1 頁

SELECT * FROM feed
WHERE user_id = 'Alice'
ORDER BY created_at DESC
LIMIT 10 OFFSET 10;  -- 第 2 頁

問題：
1. 性能問題：
   - OFFSET 10000 需要跳過 10000 條記錄 ❌
   - 越往後翻頁，越慢

2. 數據一致性問題：
   - 用戶在第 1 頁時，新帖子插入
   - 用戶翻到第 2 頁時，可能會看到重複的帖子 ❌

範例：
時刻 T1：[Post1, Post2, Post3, Post4, Post5, ...]
用戶獲取第 1 頁：[Post1, Post2, Post3]

時刻 T2：新帖子 Post0 插入
[Post0, Post1, Post2, Post3, Post4, Post5, ...]

用戶獲取第 2 頁（OFFSET 3）：[Post3, Post4, Post5]
→ Post3 重複了！ ❌
```

### Cursor 分頁（推薦）

```
Cursor 分頁原理：

使用**上一頁的最後一條記錄**作為游標（Cursor），
查詢比這條記錄更舊的帖子。

第 1 頁：
SELECT * FROM feed
WHERE user_id = 'Alice'
ORDER BY created_at DESC
LIMIT 10;

返回：[Post1(t=100), Post2(t=99), ..., Post10(t=91)]
Cursor = 91

第 2 頁：
SELECT * FROM feed
WHERE user_id = 'Alice'
  AND created_at < 91  ← 使用 Cursor
ORDER BY created_at DESC
LIMIT 10;

返回：[Post11(t=90), Post12(t=89), ..., Post20(t=81)]
Cursor = 81

優勢：
1. 性能穩定：無論翻到第幾頁，都只需掃描 10 條記錄 ✅
2. 數據一致性：新插入的帖子不會影響當前分頁 ✅
```

### Cursor 分頁實現

```go
// internal/pagination.go
package internal

import (
    "context"
    "database/sql"
    "encoding/base64"
    "encoding/json"
    "time"
)

type Cursor struct {
    CreatedAt int64  // Unix 時間戳（毫秒）
    PostID    string // 帖子 ID（用於去重）
}

type PaginatedFeed struct {
    Posts      []Post
    NextCursor string // Base64 編碼的 Cursor
    HasMore    bool
}

type PaginationService struct {
    db *sql.DB
}

// GetTimelineWithCursor 獲取動態（使用 Cursor 分頁）
func (s *PaginationService) GetTimelineWithCursor(
    ctx context.Context,
    userID string,
    cursor string,
    limit int,
) (*PaginatedFeed, error) {
    var createdAtFilter int64
    var postIDFilter string

    // 解析 Cursor
    if cursor != "" {
        c, err := decodeCursor(cursor)
        if err == nil {
            createdAtFilter = c.CreatedAt
            postIDFilter = c.PostID
        }
    }

    // 查詢帖子
    query := `
        SELECT p.id, p.user_id, p.content, p.created_at
        FROM feed f
        JOIN posts p ON f.post_id = p.id
        WHERE f.user_id = ?
    `

    args := []interface{}{userID}

    if cursor != "" {
        // 使用 Cursor 過濾
        query += ` AND (
            f.created_at < ? OR
            (f.created_at = ? AND p.id < ?)
        )`
        args = append(args, createdAtFilter, createdAtFilter, postIDFilter)
    }

    query += ` ORDER BY f.created_at DESC, p.id DESC LIMIT ?`
    args = append(args, limit+1) // 多查詢 1 條，用於判斷是否還有更多

    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var posts []Post
    for rows.Next() {
        var post Post
        var createdAt int64
        if err := rows.Scan(&post.ID, &post.UserID, &post.Content, &createdAt); err != nil {
            continue
        }
        post.CreatedAt = time.Unix(0, createdAt*int64(time.Millisecond))
        posts = append(posts, post)
    }

    // 判斷是否還有更多
    hasMore := len(posts) > limit
    if hasMore {
        posts = posts[:limit]
    }

    // 生成下一頁的 Cursor
    var nextCursor string
    if len(posts) > 0 {
        lastPost := posts[len(posts)-1]
        nextCursor = encodeCursor(Cursor{
            CreatedAt: lastPost.CreatedAt.UnixMilli(),
            PostID:    lastPost.ID,
        })
    }

    return &PaginatedFeed{
        Posts:      posts,
        NextCursor: nextCursor,
        HasMore:    hasMore,
    }, nil
}

func encodeCursor(c Cursor) string {
    data, _ := json.Marshal(c)
    return base64.StdEncoding.EncodeToString(data)
}

func decodeCursor(s string) (*Cursor, error) {
    data, err := base64.StdEncoding.DecodeString(s)
    if err != nil {
        return nil, err
    }

    var c Cursor
    if err := json.Unmarshal(data, &c); err != nil {
        return nil, err
    }

    return &c, nil
}
```

### API 示例

```go
// cmd/server/main.go
package main

import (
    "encoding/json"
    "net/http"
)

func (h *Handler) handleGetTimeline(w http.ResponseWriter, r *http.Request) {
    userID := r.URL.Query().Get("user_id")
    cursor := r.URL.Query().Get("cursor")
    limit := 10

    feed, err := h.paginationService.GetTimelineWithCursor(
        r.Context(),
        userID,
        cursor,
        limit,
    )
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(feed)
}

// 客戶端使用：
// GET /timeline?user_id=Alice
// 返回：
// {
//   "posts": [...],
//   "next_cursor": "eyJDcmVhdGVkQXQiOjE3MzMxNjAwMDB9",
//   "has_more": true
// }

// GET /timeline?user_id=Alice&cursor=eyJDcmVhdGVkQXQiOjE3MzMxNjAwMDB9
// 返回下一頁
```

---

## 第七幕：Redis 緩存優化

**Sarah**（DBA）：「雖然查詢已經很快了，但每次刷新都查詢數據庫還是有壓力。

能不能用 **Redis 緩存** Feed？」

**David**：「完全可以！這是常見的優化手段。」

### Redis 緩存策略

```
緩存架構：

1. Feed 存儲在 Redis Sorted Set：
   Key: feed:{user_id}
   Score: created_at (Unix 時間戳)
   Member: post_id

   ZADD feed:Alice 1733160000 post_123
   ZADD feed:Alice 1733159000 post_124

2. 讀取 Feed：
   ZREVRANGE feed:Alice 0 9  # 最新 10 篇

3. 緩存更新策略：
   - Write-Through：發帖時同時寫入 Redis 和 DB
   - Cache-Aside：讀取時先查 Redis，Miss 則查 DB 並回填

4. 過期策略：
   - 每個 Feed 保留最新 1000 篇
   - ZREMRANGEBYRANK feed:Alice 0 -1001  # 刪除多餘的帖子
   - 設置過期時間：EXPIRE feed:Alice 86400  # 24 小時
```

### Redis 緩存實現

```go
// internal/cache.go
package internal

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

type CacheService struct {
    rdb *redis.Client
    db  *sql.DB
}

// AddToFeedWithCache 將帖子添加到 Feed（寫入 Redis 和 DB）
func (s *CacheService) AddToFeedWithCache(
    ctx context.Context,
    userID, postID string,
    createdAt time.Time,
) error {
    // 1. 寫入數據庫
    query := "INSERT INTO feed (user_id, post_id, created_at) VALUES (?, ?, ?)"
    if _, err := s.db.ExecContext(ctx, query, userID, postID, createdAt); err != nil {
        return err
    }

    // 2. 寫入 Redis Sorted Set
    key := fmt.Sprintf("feed:%s", userID)
    score := float64(createdAt.Unix())

    if err := s.rdb.ZAdd(ctx, key, redis.Z{
        Score:  score,
        Member: postID,
    }).Err(); err != nil {
        return err
    }

    // 3. 保留最新 1000 篇（刪除多餘的）
    if err := s.rdb.ZRemRangeByRank(ctx, key, 0, -1001).Err(); err != nil {
        return err
    }

    // 4. 設置過期時間（24 小時）
    s.rdb.Expire(ctx, key, 24*time.Hour)

    return nil
}

// GetTimelineFromCache 從緩存讀取 Feed
func (s *CacheService) GetTimelineFromCache(
    ctx context.Context,
    userID string,
    limit int,
) ([]Post, error) {
    key := fmt.Sprintf("feed:%s", userID)

    // 1. 從 Redis 讀取帖子 ID（降序）
    postIDs, err := s.rdb.ZRevRange(ctx, key, 0, int64(limit-1)).Result()
    if err != nil {
        return nil, err
    }

    if len(postIDs) == 0 {
        // Cache Miss：從數據庫讀取並回填
        return s.loadFeedFromDB(ctx, userID, limit)
    }

    // 2. 批量查詢帖子詳情（可以再加一層緩存）
    posts, err := s.getPostsByIDs(ctx, postIDs)
    if err != nil {
        return nil, err
    }

    return posts, nil
}

func (s *CacheService) loadFeedFromDB(ctx context.Context, userID string, limit int) ([]Post, error) {
    // 從數據庫讀取
    query := `
        SELECT p.id, p.user_id, p.content, p.created_at
        FROM feed f
        JOIN posts p ON f.post_id = p.id
        WHERE f.user_id = ?
        ORDER BY f.created_at DESC
        LIMIT ?
    `

    rows, err := s.db.QueryContext(ctx, query, userID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var posts []Post
    key := fmt.Sprintf("feed:%s", userID)

    for rows.Next() {
        var post Post
        if err := rows.Scan(&post.ID, &post.UserID, &post.Content, &post.CreatedAt); err != nil {
            continue
        }
        posts = append(posts, post)

        // 回填 Redis
        s.rdb.ZAdd(ctx, key, redis.Z{
            Score:  float64(post.CreatedAt.Unix()),
            Member: post.ID,
        })
    }

    // 設置過期時間
    s.rdb.Expire(ctx, key, 24*time.Hour)

    return posts, nil
}

func (s *CacheService) getPostsByIDs(ctx context.Context, postIDs []string) ([]Post, error) {
    // 可以使用 Redis Hash 緩存帖子詳情
    // Key: post:{post_id}
    // Value: JSON(Post)

    var posts []Post

    for _, postID := range postIDs {
        cacheKey := fmt.Sprintf("post:%s", postID)

        // 先查 Redis
        data, err := s.rdb.Get(ctx, cacheKey).Result()
        if err == nil {
            var post Post
            if json.Unmarshal([]byte(data), &post) == nil {
                posts = append(posts, post)
                continue
            }
        }

        // Redis Miss：查數據庫
        post, err := s.getPostByID(ctx, postID)
        if err != nil {
            continue
        }

        posts = append(posts, post)

        // 回填 Redis
        if data, err := json.Marshal(post); err == nil {
            s.rdb.Set(ctx, cacheKey, data, 1*time.Hour)
        }
    }

    return posts, nil
}

func (s *CacheService) getPostByID(ctx context.Context, postID string) (Post, error) {
    query := "SELECT id, user_id, content, created_at FROM posts WHERE id = ?"
    var post Post
    err := s.db.QueryRowContext(ctx, query, postID).Scan(
        &post.ID, &post.UserID, &post.Content, &post.CreatedAt,
    )
    return post, err
}
```

### 性能提升

```
緩存前（查詢數據庫）：
- 查詢延遲：20ms（數據庫查詢）

緩存後（查詢 Redis）：
- 查詢延遲：2ms（Redis 查詢）✅

提升：10 倍

吞吐量提升：
- 數據庫：1,000 QPS（受限於連接數）
- Redis：100,000 QPS ✅

提升：100 倍
```

---

## 第八幕：真實案例 - Twitter 的架構演進

**David**：「讓我分享一個真實案例：Twitter 的 Timeline 架構演進。」

### Twitter 的三代架構

**2009 年：第一代（Pull 模型）**

```
架構：
用戶刷新 → 查詢所有關注者的最新推文 → 合併排序

問題：
- 查詢慢（用戶關注 1000 人 → 1000 次查詢）
- 數據庫壓力大
- Fail Whale 🐳（系統經常崩潰）
```

**2012 年：第二代（Fanout-on-Write）**

```
架構：
發推文 → Fanout 給所有粉絲 → 寫入 Redis

改進：
- 讀取速度快（直接從 Redis 讀取）
- 支持更高 QPS

問題：
- Justin Bieber 發推文（5000 萬粉絲）→ 系統癱瘓 ❌
```

**2013 年至今：第三代（混合模型）**

```
架構：
- 普通用戶（粉絲數 < 100 萬）：Fanout-on-Write
- 明星用戶（粉絲數 > 100 萬）：Pull 模型
- 讀取時合併

技術細節：
1. Fanout 使用 **Kafka** 異步處理
   - 發推文 → 發送到 Kafka
   - Fanout 服務消費 Kafka → 寫入 Redis

2. Feed 存儲在 **Redis Cluster**
   - 每個用戶一個 Sorted Set
   - 保留最新 800 條推文

3. 明星推文緩存
   - 緩存明星的最新 200 條推文
   - 讀取時直接從緩存合併

性能：
- QPS：30 萬+
- P99 延遲：< 100ms
- Feed 生成時間：< 5ms
```

### Twitter 的優化細節

```
1. Fanout 異步化（Kafka）：
   發推文 → 立即返回成功（不等待 Fanout 完成）
   後台 Fanout 服務處理

2. 分層 Fanout：
   - 第 1 層：Fanout 給最活躍的 10% 粉絲（實時）
   - 第 2 層：Fanout 給其餘粉絲（異步，幾秒延遲）

3. Feed 容量限制：
   - 每個用戶只保留最新 800 條推文
   - 超過的自動刪除（很少有人翻那麼遠）

4. 緩存分層：
   - L1：Redis（Feed ID 列表）
   - L2：Memcached（推文詳情）
   - L3：MySQL（持久化存儲）

5. 讀寫分離：
   - 讀：Redis Cluster（100+ 節點）
   - 寫：MySQL Master-Slave（寫主讀從）
```

---

## 第九幕：性能優化總結

**David** 總結了所有優化手段：

### 1. 模型選擇

```
┌─────────────────┬──────────────┬──────────────┬──────────────┐
│   模型          │  Pull        │  Push        │  Hybrid      │
├─────────────────┼──────────────┼──────────────┼──────────────┤
│ 讀延遲          │ 慢（1s+）     │ 快（20ms）    │ 快（60ms）    │
│ 寫延遲          │ 快（10ms）    │ 慢（1s+）     │ 快（10ms）    │
│ 存儲成本        │ 低           │ 高           │ 中           │
│ 適用場景        │ 小規模       │ 中規模       │ 大規模       │
└─────────────────┴──────────────┴──────────────┴──────────────┘

推薦：Hybrid（Twitter/Facebook 的選擇）
```

### 2. 緩存策略

```
L1: Redis（Feed ID 列表）
- 延遲：< 5ms
- 容量：100 GB
- 命中率：95%

L2: Memcached（推文詳情）
- 延遲：< 10ms
- 容量：500 GB
- 命中率：90%

L3: MySQL（持久化）
- 延遲：20-50ms
- 容量：10 TB
```

### 3. 分頁方案

```
❌ OFFSET 分頁：
- 性能差（越翻越慢）
- 數據不一致

✅ Cursor 分頁：
- 性能穩定
- 數據一致
```

### 4. 異步處理

```
Fanout 異步化（Kafka）：
- 發推文 → 立即返回（不等待 Fanout）
- 後台服務處理 Fanout
- 用戶體驗好
```

### 5. 容量規劃

```
估算（100 萬 DAU）：

Feed 存儲：
- 每個用戶 800 條 × 8 bytes (ID) = 6.4 KB
- 100 萬用戶 × 6.4 KB = 6.4 GB ✅（Redis 可以輕鬆支持）

Fanout QPS：
- 假設每個用戶每天發 10 條推文
- 每條推文 Fanout 給 200 個粉絲
- 總 Fanout：100 萬 × 10 × 200 / 86400 = 23,148 writes/s

Redis QPS：
- 寫入：23,148 writes/s
- 讀取：假設讀寫比 100:1 = 2,314,800 reads/s

需要 Redis Cluster（10-20 個節點）✅
```

---

## 第十幕：最終架構

**David** 畫出最終的架構圖：

```
┌──────────────────────────────────────────────────────────────┐
│                      News Feed 系統架構                        │
└──────────────────────────────────────────────────────────────┘

客戶端（App/Web）
    │
    ↓
┌─────────────┐
│  API Gateway │
└──────┬──────┘
       │
   ┌───┴───┐
   │       │
   ↓       ↓
寫入流程   讀取流程

【寫入流程】
發推文 API
    │
    ↓
┌─────────────┐
│  Post Service│ ← 檢查是否為明星用戶
└──────┬──────┘
       │
   ┌───┴───┐
   ↓       ↓
MySQL   Kafka Topic: new_posts
(posts)      │
             ↓
      ┌──────────────┐
      │ Fanout Service│
      │ (Consumer)    │
      └───────┬───────┘
              │
              ↓
      ┌──────────────┐
      │ Redis Cluster │ ← feed:{user_id} (Sorted Set)
      │ (Feed Storage)│
      └──────────────┘

【讀取流程】
Timeline API
    │
    ↓
┌─────────────┐
│Feed Service │
└──────┬──────┘
       │
   ┌───┴────────┐
   ↓            ↓
Redis Cluster  MySQL
(普通用戶Feed) (明星推文)
   │            │
   └─────┬──────┘
         ↓
   ┌──────────────┐
   │Merge & Rank  │ ← EdgeRank / ML 排序
   └──────┬───────┘
          ↓
   ┌──────────────┐
   │ Response     │
   └──────────────┘

【其他組件】
┌──────────────┐
│ Memcached    │ ← 推文詳情緩存
└──────────────┘

┌──────────────┐
│ CDN          │ ← 圖片/視頻
└──────────────┘
```

### 關鍵設計決策

**1. Hybrid 模型**
- 普通用戶：Fanout-on-Write（Redis）
- 明星用戶：Pull 模型（實時查詢）

**2. 異步 Fanout**
- Kafka 解耦寫入和 Fanout
- 發推文立即返回，後台異步處理

**3. 多層緩存**
- L1: Redis（Feed 列表）
- L2: Memcached（推文詳情）
- L3: MySQL（持久化）

**4. Cursor 分頁**
- 性能穩定
- 數據一致

**5. 容量限制**
- 每個 Feed 保留最新 800 條
- 自動清理舊數據

---

## 核心設計原則總結

### 1. Pull vs Push vs Hybrid

```
問題：每次讀取都實時計算太慢

方案：
- Pull（Fanout-on-Read）：讀時計算 → 讀慢寫快
- Push（Fanout-on-Write）：寫時推送 → 讀快寫慢
- Hybrid：兼顧兩者

效果：讀延遲從 1s+ 降到 60ms
```

### 2. 明星用戶處理

```
問題：明星用戶 Fanout 給 100 萬粉絲太慢

方案：明星用戶使用 Pull 模型，讀取時實時查詢

效果：寫入從 17 分鐘降到 10ms
```

### 3. Redis 緩存

```
問題：數據庫壓力大

方案：Redis Sorted Set 緩存 Feed

效果：延遲降低 10 倍，QPS 提升 100 倍
```

### 4. Cursor 分頁

```
問題：OFFSET 分頁性能差、數據不一致

方案：Cursor 分頁（基於時間戳）

效果：性能穩定、數據一致
```

### 5. 異步處理

```
問題：Fanout 阻塞用戶請求

方案：Kafka 異步 Fanout

效果：用戶體驗好（立即返回）
```

---

## 延伸閱讀

### 開源項目

- **Redis**: 高性能緩存
- **Kafka**: 分布式消息隊列
- **MySQL**: 關係型數據庫

### 論文與文章

- **EdgeRank: Facebook's News Feed Algorithm** (Facebook, 2010)
- **The Architecture Twitter Uses to Deal with 150M Active Users** (2013)
- **Scaling the Instagram Infrastructure** (Instagram, 2014)

### 相關章節

- **07-message-queue**: Kafka 消息隊列
- **05-distributed-cache**: Redis 分布式緩存
- **12-distributed-kv-store**: 分布式存儲

---

從「凌晨三點的系統告警」（P99 延遲 8.5 秒）到「秒級響應的 News Feed」（P99 < 100ms），我們經歷了：

1. **Pull 模型** → 每次實時計算，太慢 ❌
2. **Fanout-on-Write** → 讀快但明星用戶寫慢 ❌
3. **混合模型** → 兼顧讀寫性能 ✅
4. **Redis 緩存** → 降低延遲 10 倍 ✅
5. **Cursor 分頁** → 性能穩定、數據一致 ✅
6. **異步 Fanout** → 用戶體驗好 ✅

**記住：選擇合適的模型比優化細節更重要。Twitter 的 Hybrid 模型是經過實踐驗證的最佳方案。**

**核心理念：讀多寫少的場景，用空間換時間（預計算）！**
