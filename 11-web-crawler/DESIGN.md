# Web Crawler 系統設計文檔

## 週一早上的融資路演

2024 年 6 月 3 日上午 10:00，創業公司「省錢達人」的融資路演。

CEO Alice 向投資人展示產品：

「我們的比價網站能幫用戶找到全網最低價！」

她打開網站演示：

```
商品：iPhone 15 Pro Max 256GB

📊 價格比較：
✅ 蝦皮：NT$ 38,900（最低價）
   PChome：NT$ 39,900
   momo：NT$ 40,500
   露天：NT$ 39,500

💰 省下：NT$ 1,600
```

投資人很感興趣：「你們爬了多少個商品？」

「目前... 500 個。」Alice 尷尬地說。

「才 500 個？你們的競爭對手 BigSaver 有 500 萬個商品！」

「我們會儘快擴展！」Alice 強裝鎮定。

會議結束後，她立刻召集技術團隊。

「我們需要在一週內爬到 100 萬個商品，否則拿不到投資！」

後端工程師 Tom 苦笑：「我們現在的爬蟲... 每秒只能爬 1 個頁面。」

**計算：**
```
當前速度：1 URL/秒
目標：100 萬個商品
需要時間：1,000,000 ÷ 1 = 1,000,000 秒 ≈ 11.5 天

問題：我們只有 7 天！
```

## 第一次災難：單線程爬蟲太慢（2024/06/03）

### 最初的實現：簡單的 HTTP 請求

Tom 當初寫的爬蟲非常簡單：

```go
package main

import (
    "fmt"
    "io"
    "net/http"
    "regexp"
)

func main() {
    urls := []string{
        "https://shopee.tw/product/123",
        "https://shopee.tw/product/456",
        // ... 更多 URL
    }

    for _, url := range urls {
        crawl(url)
    }
}

func crawl(url string) {
    // 1. 發送 HTTP 請求
    resp, err := http.Get(url)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }
    defer resp.Body.Close()

    // 2. 讀取 HTML
    body, _ := io.ReadAll(resp.Body)
    html := string(body)

    // 3. 解析價格（簡單的正則表達式）
    re := regexp.MustCompile(`NT\$ ([\d,]+)`)
    matches := re.FindStringSubmatch(html)
    if len(matches) > 1 {
        price := matches[1]
        fmt.Printf("URL: %s, Price: NT$ %s\n", url, price)
    }
}
```

「就這樣，一個接一個爬！」Tom 說。

### 性能分析

```
單個請求時序：
1. DNS 查詢：50ms
2. TCP 連接：100ms
3. HTTP 請求/響應：500ms
4. HTML 解析：50ms
5. 總計：700ms ≈ 1 秒/URL

實際測試：
- 爬取 100 個 URL：耗時 120 秒
- 平均速度：0.83 URL/秒

100 萬個商品需要：
1,000,000 ÷ 0.83 = 1,204,819 秒 ≈ 14 天 ❌
```

**為什麼這麼慢？**

```
問題 1：單線程，同步阻塞
- 每次只爬 1 個 URL
- 等待 HTTP 響應時，CPU 閒置
- 網絡 I/O 成為瓶頸

問題 2：沒有並發
- CPU：1% 使用率（幾乎閒置）
- 網絡：等待時間佔 95%
- 資源浪費

類比：
單線程爬蟲 = 餐廳只有 1 個服務生
顧客點餐後，服務生站著等廚房出菜（浪費）
```

Alice 焦急：「有沒有辦法加快？」

Tom 想了想：「用多線程？」

## 第二次嘗試：多線程並發爬取（2024/06/04）

### 思路

「網絡 I/O 等待時，讓其他線程繼續爬！」

```go
func main() {
    urls := loadURLs() // 假設有 10,000 個 URL

    // 使用 goroutine 並發爬取
    var wg sync.WaitGroup
    for _, url := range urls {
        wg.Add(1)
        go func(u string) {
            defer wg.Done()
            crawl(u)
        }(url)
    }
    wg.Wait()
}
```

「每個 URL 一個 goroutine，全速爬取！」Tom 自信滿滿。

### 上線測試

**2024/06/04 15:00** - 啟動爬蟲

**15:01** - CPU 飆升到 100%

**15:02** - 內存從 100 MB 飆升到 8 GB

**15:03** - 服務器崩潰（OOM Killed）

### 問題分析

```
災難數據：
- 創建 goroutine 數：10,000 個
- 每個 goroutine 內存：約 2 KB（棧空間）
- 但每個 HTTP 連接：約 4 KB（緩衝）+ 大量對象分配
- 總內存：10,000 × 1 MB（平均每個連接） = 10 GB ❌

問題：
1. Goroutine 洪水：創建 10,000 個 goroutine
2. 連接爆炸：同時打開 10,000 個 TCP 連接
3. 目標網站：收到 10,000 個同時請求 → 認為是 DDoS 攻擊
4. 結果：IP 被封禁 ❌
```

Tom 收到蝦皮的郵件：

```
主旨：您的 IP 已被封禁

親愛的用戶：

我們檢測到您的 IP（203.0.113.42）在 1 分鐘內發送了
10,000 個請求，疑似爬蟲或 DDoS 攻擊。

您的 IP 已被封禁 24 小時。

如有疑問，請聯繫客服。

- 蝦皮技術團隊
```

Alice 暴怒：「你把我們的 IP 搞到被封了？！」

### 凌晨的反思

Tom 凌晨 2 點還在辦公室，看著監控數據。

他意識到兩個問題：

```
問題 1：無節制的並發
- 10,000 個 goroutine 同時爬取
- 目標網站無法承受
- 需要「限流」

問題 2：重複爬取
- 某些 URL 被爬了多次（因為從不同頁面發現）
- 浪費資源
- 需要「去重」
```

## 第三次改進：有限並發 + 去重（2024/06/05）

### 改進 1：Worker Pool（工作池）

```go
type Crawler struct {
    workerCount int
    urlQueue    chan string
    visited     map[string]bool
    mu          sync.Mutex
}

func NewCrawler(workerCount int) *Crawler {
    return &Crawler{
        workerCount: workerCount,
        urlQueue:    make(chan string, 1000),
        visited:     make(map[string]bool),
    }
}

func (c *Crawler) Start() {
    // 啟動固定數量的 worker
    var wg sync.WaitGroup
    for i := 0; i < c.workerCount; i++ {
        wg.Add(1)
        go func(workerID int) {
            defer wg.Done()
            c.worker(workerID)
        }(i)
    }

    wg.Wait()
}

func (c *Crawler) worker(id int) {
    for url := range c.urlQueue {
        // 檢查是否已爬取（去重）
        c.mu.Lock()
        if c.visited[url] {
            c.mu.Unlock()
            continue // 跳過已爬取的 URL
        }
        c.visited[url] = true
        c.mu.Unlock()

        // 爬取
        c.crawl(url)

        // 禮貌性：每次爬取後等待 1 秒
        time.Sleep(1 * time.Second)
    }
}

func (c *Crawler) AddURL(url string) {
    c.urlQueue <- url
}
```

**改進點：**
```
1. Worker Pool：
   - 只創建 10 個 worker goroutine
   - 共享 URL 隊列
   - 避免 goroutine 洪水

2. 去重：
   - 使用 map[string]bool 記錄已訪問的 URL
   - 避免重複爬取

3. 禮貌性（Politeness）：
   - 每次爬取後等待 1 秒
   - 避免對目標網站造成壓力
```

### 測試結果

```
配置：10 個 worker

性能：
- 吞吐量：10 URL/秒（每個 worker 1 秒爬 1 個）
- 100 萬個商品：1,000,000 ÷ 10 = 100,000 秒 ≈ 27.8 小時

結果：
✅ 不再被封禁
✅ 內存穩定在 200 MB
❌ 仍然太慢（需要 27.8 小時）
```

Tom 向 Alice 報告：「我們現在每秒爬 10 個，27 小時能完成。」

Alice：「還是太慢！能再快一點嗎？」

Tom：「可以增加 worker 數量，但...」

他打開 `robots.txt`：

```
https://shopee.tw/robots.txt

User-agent: *
Crawl-delay: 1

Disallow: /checkout
Disallow: /cart
Disallow: /user
```

「`Crawl-delay: 1` 表示每次爬取要間隔至少 1 秒。」Tom 解釋。

「如果我們不遵守呢？」Alice 問。

「會被封禁，而且不道德。」Tom 嚴肅地說。

## 第四次災難：內存爆炸（2024/06/06）

### 背景：爬取深度增加

產品經理：「我們不只要爬商品頁，還要爬分類頁、搜尋結果頁！」

Tom 修改代碼，支持爬取頁面中的所有鏈接：

```go
func (c *Crawler) crawl(url string) {
    // 1. 爬取頁面
    html := fetchHTML(url)

    // 2. 解析所有鏈接
    links := extractLinks(html, url)

    // 3. 將新鏈接加入隊列
    for _, link := range links {
        c.AddURL(link)
    }
}
```

結果...

### 災難數據（2024/06/06 20:00）

```
啟動爬蟲（種子 URL：蝦皮首頁）

20:00 - 爬取隊列：1 個 URL
20:01 - 爬取隊列：237 個 URL（首頁的所有鏈接）
20:02 - 爬取隊列：5,489 個 URL
20:05 - 爬取隊列：128,394 個 URL
20:10 - 爬取隊列：3,247,891 個 URL
20:15 - 內存：12 GB（visited map 爆炸）
20:20 - 服務器崩潰（OOM）
```

**問題分析：**

```
URL 爆炸式增長：
- 每個頁面平均有 50 個鏈接
- 第 1 層：1 個 URL
- 第 2 層：50 個 URL
- 第 3 層：2,500 個 URL
- 第 4 層：125,000 個 URL
- 第 5 層：6,250,000 個 URL ❌

visited map 內存占用：
- 每個 URL 平均 100 bytes（字符串）
- 600 萬個 URL × 100 bytes = 600 MB
- 加上 map 開銷（指針、哈希表）：約 1.2 GB

問題：
1. URL 數量爆炸
2. map[string]bool 無法擴展到千萬級
3. 內存不足
```

Tom 崩潰了：「URL 太多了，根本存不下！」

### 靈感：布隆過濾器（Bloom Filter）

資深工程師 David 走過來：「用 Bloom Filter 啊。」

「什麼是 Bloom Filter？」Tom 問。

David 在白板上畫圖：

```
Bloom Filter：概率型數據結構

原理：
1. 用多個哈希函數映射 URL 到 bit 數組
2. 插入時：將對應位置設為 1
3. 查詢時：檢查所有位置是否為 1

範例：
Bit Array（10 位）：[0,0,0,0,0,0,0,0,0,0]

插入 "https://shopee.tw/product/123"：
- hash1(url) % 10 = 3 → bit[3] = 1
- hash2(url) % 10 = 7 → bit[7] = 1
- hash3(url) % 10 = 2 → bit[2] = 1

結果：[0,0,1,1,0,0,0,1,0,0]

查詢 "https://shopee.tw/product/123"：
- bit[3] = 1 ✅
- bit[7] = 1 ✅
- bit[2] = 1 ✅
- 結論：「可能」存在（有誤判可能）

查詢 "https://shopee.tw/product/999"：
- hash1 → bit[5] = 0 ❌
- 結論：「一定」不存在
```

**Bloom Filter 特性：**
```
優勢：
- 內存極小：600 萬個 URL，只需 約 9 MB（vs map 的 1.2 GB）
- 查詢極快：O(k)，k = 哈希函數數量（通常 3-5）

劣勢：
- 有誤判：可能將未訪問的 URL 判斷為已訪問（機率可控）
- 無法刪除：一旦插入，無法移除（除非用 Counting Bloom Filter）

誤判率計算：
- m = 位數組大小（bits）
- n = 插入元素數量
- k = 哈希函數數量
- 誤判率 ≈ (1 - e^(-kn/m))^k

範例：
- 600 萬個 URL
- 使用 72,000,000 bits（9 MB）
- 3 個哈希函數
- 誤判率 ≈ 1% ✅
```

### 實現 Bloom Filter

```go
package internal

import (
    "hash/fnv"
    "math"
)

type BloomFilter struct {
    bitArray []bool
    size     uint
    hashCount int
}

func NewBloomFilter(expectedElements int, falsePositiveRate float64) *BloomFilter {
    // 計算最佳參數
    size := optimalSize(expectedElements, falsePositiveRate)
    hashCount := optimalHashCount(size, expectedElements)

    return &BloomFilter{
        bitArray:  make([]bool, size),
        size:      uint(size),
        hashCount: hashCount,
    }
}

// 計算最佳 bit 數組大小
func optimalSize(n int, p float64) int {
    m := -(float64(n) * math.Log(p)) / math.Pow(math.Log(2), 2)
    return int(math.Ceil(m))
}

// 計算最佳哈希函數數量
func optimalHashCount(m, n int) int {
    k := (float64(m) / float64(n)) * math.Log(2)
    return int(math.Ceil(k))
}

// 插入 URL
func (bf *BloomFilter) Add(url string) {
    for i := 0; i < bf.hashCount; i++ {
        hash := bf.hash(url, uint(i))
        bf.bitArray[hash] = true
    }
}

// 檢查 URL 是否存在
func (bf *BloomFilter) Contains(url string) bool {
    for i := 0; i < bf.hashCount; i++ {
        hash := bf.hash(url, uint(i))
        if !bf.bitArray[hash] {
            return false // 一定不存在
        }
    }
    return true // 可能存在（有誤判）
}

// 哈希函數（使用 FNV-1a + 偏移）
func (bf *BloomFilter) hash(url string, seed uint) uint {
    h := fnv.New64a()
    h.Write([]byte(url))
    hash := h.Sum64() + uint64(seed)*0x9e3779b97f4a7c15 // 魔數
    return uint(hash % uint64(bf.size))
}
```

### 性能對比（2024/06/07 測試）

```
場景：去重 600 萬個 URL

方案 A：map[string]bool
- 內存：1.2 GB
- 插入：O(1) 平均，最壞 O(N)
- 查詢：O(1) 平均
- 準確率：100%

方案 B：Bloom Filter
- 內存：9 MB（節省 99.2%）
- 插入：O(k)，k=3
- 查詢：O(k)
- 準確率：99%（1% 誤判）

結果：
✅ 內存從 1.2 GB → 9 MB
✅ 可以處理千萬級 URL
⚠️ 1% 的 URL 會被誤判為「已訪問」（可接受）
```

Tom 興奮地實現了 Bloom Filter，內存問題解決了！

但新問題出現了...

## 第五次挑戰：robots.txt 解析（2024/06/08）

### 背景：被另一個網站封禁

Tom 高興地擴大爬取範圍，加入了 momo、PChome、露天等網站。

結果收到 PChome 的警告信：

```
主旨：違反 robots.txt 警告

您的爬蟲違反了我們的 robots.txt 規則：

1. 您爬取了 /user/profile（明確禁止）
2. 您沒有遵守 Crawl-delay: 2（應間隔 2 秒）
3. 您的 User-Agent 是空的（應設置合法的 User-Agent）

請立即修正，否則我們將封禁您的 IP。

- PChome 技術團隊
```

Tom 慌了：「我根本沒看 robots.txt！」

### 什麼是 robots.txt？

David 解釋：

```
robots.txt：網站給爬蟲的規則文件

位置：網站根目錄
範例：https://www.pchome.com.tw/robots.txt

內容：
User-agent: *
Crawl-delay: 2

Disallow: /user
Disallow: /checkout
Disallow: /cart
Disallow: /admin

Allow: /product
Allow: /search

含義：
- User-agent: * → 適用所有爬蟲
- Crawl-delay: 2 → 每次請求間隔至少 2 秒
- Disallow: /user → 禁止爬取 /user 路徑
- Allow: /product → 允許爬取 /product 路徑
```

**為什麼要遵守 robots.txt？**

```
1. 法律：某些國家（如美國）違反 robots.txt 可能違法
2. 道德：尊重網站所有者的意願
3. 實務：不遵守會被封禁

Google 爬蟲（Googlebot）：
- 100% 遵守 robots.txt
- 設置清晰的 User-Agent
- 限制請求頻率（Politeness）
```

### 實現 robots.txt 解析

```go
package internal

import (
    "bufio"
    "net/http"
    "strings"
    "time"
)

type RobotsTxt struct {
    rules      map[string]*Rules // 按 User-Agent 分組
    cache      map[string]*RobotsTxt
    mu         sync.RWMutex
}

type Rules struct {
    Disallow    []string
    Allow       []string
    CrawlDelay  time.Duration
}

func NewRobotsTxt() *RobotsTxt {
    return &RobotsTxt{
        rules: make(map[string]*Rules),
        cache: make(map[string]*RobotsTxt),
    }
}

// 獲取並解析 robots.txt
func (r *RobotsTxt) Fetch(baseURL string) error {
    robotsURL := baseURL + "/robots.txt"

    resp, err := http.Get(robotsURL)
    if err != nil {
        // robots.txt 不存在，默認允許所有
        return nil
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return nil // robots.txt 不存在
    }

    // 解析 robots.txt
    scanner := bufio.NewScanner(resp.Body)
    var currentAgent string
    currentRules := &Rules{}

    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())

        // 跳過註釋和空行
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }

        parts := strings.SplitN(line, ":", 2)
        if len(parts) != 2 {
            continue
        }

        key := strings.TrimSpace(strings.ToLower(parts[0]))
        value := strings.TrimSpace(parts[1])

        switch key {
        case "user-agent":
            if currentAgent != "" {
                r.rules[currentAgent] = currentRules
            }
            currentAgent = value
            currentRules = &Rules{}

        case "disallow":
            if value != "" {
                currentRules.Disallow = append(currentRules.Disallow, value)
            }

        case "allow":
            currentRules.Allow = append(currentRules.Allow, value)

        case "crawl-delay":
            if delay, err := time.ParseDuration(value + "s"); err == nil {
                currentRules.CrawlDelay = delay
            }
        }
    }

    // 保存最後一個 User-Agent 的規則
    if currentAgent != "" {
        r.rules[currentAgent] = currentRules
    }

    return nil
}

// 檢查 URL 是否允許爬取
func (r *RobotsTxt) IsAllowed(userAgent, path string) bool {
    r.mu.RLock()
    defer r.mu.RUnlock()

    // 查找規則（先查具體 User-Agent，再查 *）
    rules := r.rules[userAgent]
    if rules == nil {
        rules = r.rules["*"]
    }
    if rules == nil {
        return true // 沒有規則，默認允許
    }

    // 檢查 Allow 規則（優先級高）
    for _, allow := range rules.Allow {
        if strings.HasPrefix(path, allow) {
            return true
        }
    }

    // 檢查 Disallow 規則
    for _, disallow := range rules.Disallow {
        if strings.HasPrefix(path, disallow) {
            return false
        }
    }

    return true // 沒有匹配的規則，默認允許
}

// 獲取 Crawl-delay
func (r *RobotsTxt) GetCrawlDelay(userAgent string) time.Duration {
    r.mu.RLock()
    defer r.mu.RUnlock()

    rules := r.rules[userAgent]
    if rules == nil {
        rules = r.rules["*"]
    }
    if rules == nil {
        return 0
    }

    return rules.CrawlDelay
}
```

### 集成到爬蟲

```go
type Crawler struct {
    // ... 其他字段

    robotsTxt   map[string]*RobotsTxt // 按域名緩存
    userAgent   string
}

func NewCrawler(workerCount int, userAgent string) *Crawler {
    return &Crawler{
        // ...
        robotsTxt: make(map[string]*RobotsTxt),
        userAgent: userAgent,
    }
}

func (c *Crawler) crawl(urlStr string) {
    parsedURL, _ := url.Parse(urlStr)
    domain := parsedURL.Scheme + "://" + parsedURL.Host

    // 1. 獲取 robots.txt（帶緩存）
    c.mu.Lock()
    robotsTxt := c.robotsTxt[domain]
    if robotsTxt == nil {
        robotsTxt = NewRobotsTxt()
        robotsTxt.Fetch(domain)
        c.robotsTxt[domain] = robotsTxt
    }
    c.mu.Unlock()

    // 2. 檢查是否允許爬取
    if !robotsTxt.IsAllowed(c.userAgent, parsedURL.Path) {
        log.Printf("Disallowed by robots.txt: %s", urlStr)
        return
    }

    // 3. 遵守 Crawl-delay
    delay := robotsTxt.GetCrawlDelay(c.userAgent)
    if delay > 0 {
        time.Sleep(delay)
    }

    // 4. 設置 User-Agent
    req, _ := http.NewRequest("GET", urlStr, nil)
    req.Header.Set("User-Agent", c.userAgent)

    // 5. 發送請求
    resp, _ := http.DefaultClient.Do(req)
    defer resp.Body.Close()

    // ... 處理響應
}
```

## 第六次挑戰：URL 管理混亂（2024/06/10）

### 問題：BFS vs 優先級

產品經理：「我們要優先爬商品頁，而不是分類頁、關於我們等無關頁面。」

Tom 查看當前的 URL 隊列：

```
隊列（FIFO，先進先出）：
1. https://shopee.tw/about（關於我們）
2. https://shopee.tw/terms（服務條款）
3. https://shopee.tw/privacy（隱私政策）
4. https://shopee.tw/product/123（商品頁）❗重要
5. https://shopee.tw/product/456（商品頁）❗重要
...

問題：
- 商品頁被排在後面
- 浪費時間爬取無關頁面
- 需要「優先級」機制
```

### 方案：URL Frontier（URL 邊界）

David 介紹：「這是 Google 用的架構：URL Frontier。」

```
URL Frontier：管理待爬取 URL 的組件

設計目標：
1. 優先級：重要頁面優先爬取
2. 禮貌性（Politeness）：同一域名請求間隔足夠長
3. 新鮮度（Freshness）：定期重新爬取（如價格更新）
4. 去重：避免重複爬取

架構：

              ┌─────────────────┐
              │  URL Discovery  │（發現新 URL）
              └────────┬────────┘
                       ↓
              ┌─────────────────┐
              │  URL Filter     │（去重、robots.txt）
              │  (Bloom Filter) │
              └────────┬────────┘
                       ↓
              ┌─────────────────┐
              │  URL Frontier   │
              │                 │
              │ ┌─────────────┐ │
              │ │ 優先級隊列   │ │
              │ │   P0: 商品頁 │ │
              │ │   P1: 分類頁 │ │
              │ │   P2: 其他   │ │
              │ └─────────────┘ │
              │                 │
              │ ┌─────────────┐ │
              │ │ 禮貌性隊列   │ │
              │ │ shopee.tw   │ │
              │ │ momo.tw     │ │
              │ │ pchome.tw   │ │
              │ └─────────────┘ │
              └────────┬────────┘
                       ↓
              ┌─────────────────┐
              │     Workers     │（並發爬取）
              └─────────────────┘
```

### 實現優先級隊列

```go
package internal

import (
    "container/heap"
    "net/url"
)

// URLItem 待爬取的 URL
type URLItem struct {
    URL      string
    Priority int       // 優先級（數字越小越高）
    Depth    int       // 深度（從種子頁的距離）
    Index    int       // 在堆中的索引
}

// PriorityQueue 優先級隊列（基於 container/heap）
type PriorityQueue []*URLItem

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
    // 優先級數字越小越優先
    if pq[i].Priority != pq[j].Priority {
        return pq[i].Priority < pq[j].Priority
    }
    // 優先級相同，深度越小越優先（BFS）
    return pq[i].Depth < pq[j].Depth
}

func (pq PriorityQueue) Swap(i, j int) {
    pq[i], pq[j] = pq[j], pq[i]
    pq[i].Index = i
    pq[j].Index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
    n := len(*pq)
    item := x.(*URLItem)
    item.Index = n
    *pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
    old := *pq
    n := len(old)
    item := old[n-1]
    old[n-1] = nil
    item.Index = -1
    *pq = old[0 : n-1]
    return item
}

// URLFrontier URL 邊界
type URLFrontier struct {
    priorityQueue *PriorityQueue
    bloomFilter   *BloomFilter
    mu            sync.Mutex
}

func NewURLFrontier(expectedURLs int) *URLFrontier {
    pq := &PriorityQueue{}
    heap.Init(pq)

    return &URLFrontier{
        priorityQueue: pq,
        bloomFilter:   NewBloomFilter(expectedURLs, 0.01),
    }
}

// Add 添加 URL（帶優先級）
func (uf *URLFrontier) Add(urlStr string, priority, depth int) bool {
    uf.mu.Lock()
    defer uf.mu.Unlock()

    // 去重檢查
    if uf.bloomFilter.Contains(urlStr) {
        return false // 可能已存在
    }

    uf.bloomFilter.Add(urlStr)

    item := &URLItem{
        URL:      urlStr,
        Priority: priority,
        Depth:    depth,
    }

    heap.Push(uf.priorityQueue, item)
    return true
}

// Next 獲取下一個待爬取的 URL（優先級最高）
func (uf *URLFrontier) Next() (*URLItem, bool) {
    uf.mu.Lock()
    defer uf.mu.Unlock()

    if uf.priorityQueue.Len() == 0 {
        return nil, false
    }

    item := heap.Pop(uf.priorityQueue).(*URLItem)
    return item, true
}

// Size 返回隊列大小
func (uf *URLFrontier) Size() int {
    uf.mu.Lock()
    defer uf.mu.Unlock()
    return uf.priorityQueue.Len()
}
```

### 優先級策略

```go
// 計算 URL 優先級
func calculatePriority(urlStr string) int {
    u, _ := url.Parse(urlStr)

    // 優先級 0：商品頁（最高）
    if strings.Contains(u.Path, "/product/") ||
       strings.Contains(u.Path, "/item/") {
        return 0
    }

    // 優先級 1：搜尋結果、分類頁
    if strings.Contains(u.Path, "/search") ||
       strings.Contains(u.Path, "/category") {
        return 1
    }

    // 優先級 2：首頁
    if u.Path == "/" {
        return 2
    }

    // 優先級 3：其他（最低）
    return 3
}
```

### 禮貌性隊列（Per-Host Queue）

```go
// HostQueue 按域名分組的隊列
type HostQueue struct {
    queues      map[string]*queue.Queue
    lastAccess  map[string]time.Time
    crawlDelay  map[string]time.Duration
    mu          sync.Mutex
}

func NewHostQueue() *HostQueue {
    return &HostQueue{
        queues:     make(map[string]*queue.Queue),
        lastAccess: make(map[string]time.Time),
        crawlDelay: make(map[string]time.Duration),
    }
}

// Add 添加 URL 到對應域名的隊列
func (hq *HostQueue) Add(urlStr string) {
    u, _ := url.Parse(urlStr)
    host := u.Host

    hq.mu.Lock()
    defer hq.mu.Unlock()

    if hq.queues[host] == nil {
        hq.queues[host] = queue.New()
    }

    hq.queues[host].Push(urlStr)
}

// Next 獲取下一個可爬取的 URL（遵守 Crawl-delay）
func (hq *HostQueue) Next() (string, bool) {
    hq.mu.Lock()
    defer hq.mu.Unlock()

    now := time.Now()

    // 遍歷所有域名隊列
    for host, q := range hq.queues {
        if q.Len() == 0 {
            continue
        }

        // 檢查是否滿足 Crawl-delay
        lastAccess := hq.lastAccess[host]
        delay := hq.crawlDelay[host]
        if delay == 0 {
            delay = 1 * time.Second // 默認 1 秒
        }

        if now.Sub(lastAccess) < delay {
            continue // 還沒到時間
        }

        // 取出 URL
        urlStr := q.Pop().(string)
        hq.lastAccess[host] = now

        return urlStr, true
    }

    return "", false // 所有隊列都空或都在等待
}
```

## 第七次挑戰：DNS 查詢成為瓶頸（2024/06/12）

### 問題發現

Tom 發現爬蟲速度還是不夠快，profiling 後發現：

```
性能分析（10 個 worker）：
- DNS 查詢：45%（450ms）
- TCP 連接：20%（200ms）
- HTTP 請求/響應：30%（300ms）
- HTML 解析：5%（50ms）

瓶頸：DNS 查詢佔了 45% 的時間！
```

**為什麼 DNS 這麼慢？**

```
每次爬取都要查詢 DNS：
1. Worker 1 爬 https://shopee.tw/product/123
   → DNS 查詢 shopee.tw → 103.74.120.23（50ms）

2. Worker 2 爬 https://shopee.tw/product/456
   → DNS 查詢 shopee.tw → 103.74.120.23（50ms）重複！

3. Worker 3 爬 https://shopee.tw/product/789
   → DNS 查詢 shopee.tw → 103.74.120.23（50ms）重複！

問題：
- 相同域名重複查詢
- DNS 查詢耗時 50ms+
- 浪費資源
```

### 方案：DNS 緩存

```go
package internal

import (
    "context"
    "net"
    "sync"
    "time"
)

type DNSCache struct {
    cache map[string]*DNSEntry
    mu    sync.RWMutex
}

type DNSEntry struct {
    IPs       []string
    ExpireAt  time.Time
}

func NewDNSCache() *DNSCache {
    return &DNSCache{
        cache: make(map[string]*DNSEntry),
    }
}

// Resolve 解析域名（帶緩存）
func (dc *DNSCache) Resolve(hostname string) ([]string, error) {
    // 1. 檢查緩存
    dc.mu.RLock()
    entry := dc.cache[hostname]
    dc.mu.RUnlock()

    if entry != nil && time.Now().Before(entry.ExpireAt) {
        return entry.IPs, nil // 緩存命中
    }

    // 2. 緩存未命中，查詢 DNS
    ips, err := net.DefaultResolver.LookupHost(context.Background(), hostname)
    if err != nil {
        return nil, err
    }

    // 3. 存入緩存（TTL 5 分鐘）
    dc.mu.Lock()
    dc.cache[hostname] = &DNSEntry{
        IPs:      ips,
        ExpireAt: time.Now().Add(5 * time.Minute),
    }
    dc.mu.Unlock()

    return ips, nil
}
```

### 自定義 HTTP Transport

```go
func NewHTTPClientWithDNSCache(dnsCache *DNSCache) *http.Client {
    dialer := &net.Dialer{
        Timeout:   30 * time.Second,
        KeepAlive: 30 * time.Second,
    }

    transport := &http.Transport{
        DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
            // 解析域名和端口
            host, port, _ := net.SplitHostPort(addr)

            // 使用 DNS 緩存
            ips, err := dnsCache.Resolve(host)
            if err != nil {
                return nil, err
            }

            // 使用第一個 IP
            ip := ips[0]
            addr = net.JoinHostPort(ip, port)

            // 建立連接
            return dialer.DialContext(ctx, network, addr)
        },
        MaxIdleConns:        100,
        IdleConnTimeout:     90 * time.Second,
        TLSHandshakeTimeout: 10 * time.Second,
    }

    return &http.Client{
        Transport: transport,
        Timeout:   60 * time.Second,
    }
}
```

### 性能對比（2024/06/13 測試）

```
場景：爬取 10,000 個 URL（來自 10 個不同域名）

無 DNS 緩存：
- 總 DNS 查詢：10,000 次
- DNS 查詢時間：10,000 × 50ms = 500 秒
- 總耗時：約 15 分鐘

有 DNS 緩存：
- 總 DNS 查詢：10 次（每個域名一次）
- DNS 查詢時間：10 × 50ms = 0.5 秒
- 總耗時：約 8 分鐘

提升：15 分鐘 → 8 分鐘（快 47%）
```

## 新的挑戰：分布式爬蟲（擴展到 100x）

### 當前架構的瓶頸

```
單機爬蟲容量：
- 10 個 worker
- 每個 worker 1 秒爬 1 個 URL（遵守 Crawl-delay）
- 吞吐量：10 URL/秒

100 萬個商品：
- 需要時間：1,000,000 ÷ 10 = 100,000 秒 ≈ 27.8 小時 ✅

問題：
- 如果要爬 1 億個頁面呢？→ 115 天 ❌
- 單機故障 → 所有爬取停止
- 無法橫向擴展
```

### 10x 擴展：多機協調

```
架構變化：

當前（單機）：
Crawler → URL Frontier (內存) → Workers → 目標網站

優化後（多機）：
               ┌─ Crawler 1 ─┐
               ├─ Crawler 2 ─┤
               ├─ Crawler 3 ─┤→ 目標網站
               └─ Crawler 4 ─┘
                      ↑
            ┌─────────┴─────────┐
            │                   │
    ┌───────────────┐   ┌───────────────┐
    │ Redis         │   │ URL Queue     │
    │ (Bloom Filter)│   │ (RabbitMQ)    │
    └───────────────┘   └───────────────┘

分工：
1. 中央 URL Queue（RabbitMQ / Kafka）：
   - 存儲待爬取的 URL
   - 多個 Crawler 消費

2. 分布式去重（Redis Bloom Filter）：
   - 共享的 Bloom Filter
   - 避免多台機器重複爬取

3. URL 分配策略：
   - 按域名 hash 分配（同一域名的 URL 分配到同一台機器）
   - 保證 Politeness（同一域名的請求由同一台機器處理）

容量：
- 10 台機器 × 10 URL/秒 = 100 URL/秒
- 1 億頁面：1億 ÷ 100 = 100 萬秒 ≈ 11.5 天
```

### 100x 擴展：專業爬蟲架構

```
Google 級別的爬蟲架構：

          ┌────────────────┐
          │  URL Scheduler │（中央調度器）
          │  - 優先級管理   │
          │  - 新鮮度更新   │
          └────────┬───────┘
                   ↓
          ┌────────────────┐
          │  URL Frontier  │
          │  (分布式隊列)    │
          │  - Kafka       │
          └────────┬───────┘
                   ↓
       ┌───────────┴───────────┐
       ↓                       ↓
┌─────────────┐       ┌─────────────┐
│ Crawler     │  ...  │ Crawler     │（100 台）
│ Cluster 1   │       │ Cluster 10  │
│ (10 機器)    │       │ (10 機器)   │
└──────┬──────┘       └──────┬──────┘
       ↓                      ↓
┌─────────────┐       ┌─────────────┐
│ DNS Cache   │       │ DNS Cache   │
│ (本地)       │       │ (本地)      │
└──────┬──────┘       └──────┬──────┘
       ↓                      ↓
    目標網站              目標網站

    ┌────────────────────────┐
    │ 數據存儲               │
    │ - HBase (爬取的HTML)   │
    │ - S3 (原始文件)        │
    │ - Elasticsearch (索引) │
    └────────────────────────┘

優化：
1. 地理分布：
   - 不同地區部署 Crawler（減少延遲）
   - 爬取該地區的網站

2. 智能重試：
   - 失敗的 URL 自動重試（指數退避）
   - 多次失敗 → 降低優先級

3. 內容指紋：
   - 檢測頁面內容變化（SimHash）
   - 只爬取有更新的頁面

4. 增量爬取：
   - 定期重新爬取（如每天）
   - 價格監控、新聞更新

容量：
- 100 台機器 × 10 URL/秒 = 1,000 URL/秒
- 1 億頁面：1億 ÷ 1,000 = 10 萬秒 ≈ 27.8 小時
- 每天爬取：1,000 × 86,400 = 8,640 萬頁面
```

## 真實案例：Google 爬蟲的演進

### Google 爬蟲（Googlebot）的技術細節

**1998 年（初版 Google）：**
```
架構：
- 單機爬蟲
- 簡單的 BFS
- 爬取速度：約 100 頁/秒

問題：
- 速度慢
- 無法處理海量網頁
```

**2005 年（分布式爬蟲）：**
```
架構：
- 分布式架構（數百台機器）
- BigTable 存儲爬取數據
- 爬取速度：約 10 萬頁/秒

優化：
- URL Frontier（優先級隊列）
- 分布式去重（Bloom Filter）
- DNS 緩存
```

**2020 年（現代 Googlebot）：**
```
架構：
- 全球分布（數千台機器）
- Caffeine 索引系統（實時索引）
- 爬取速度：約 100 萬頁/秒

技術：
- JavaScript 渲染（Headless Chrome）
- 移動優先索引
- HTTP/2 支持
- 智能爬取頻率（Machine Learning）

規模：
- 索引頁面：數千億頁
- 每天新增：數十億頁
- 全球數據中心：20+
```

**Googlebot 的禮貌性策略：**
```
1. 嚴格遵守 robots.txt
2. 動態調整爬取頻率：
   - 網站響應快 → 增加頻率
   - 網站響應慢/錯誤 → 減少頻率
3. 避免高峰時段（如黑色星期五）
4. 尊重 Crawl-delay（即使很長）
```

參考資料：
- [How Google Search Works](https://www.google.com/search/howsearchworks/crawling-indexing/)
- Google 專利：US 6,285,999 B1（Method for node ranking in a linked database）

## 總結與對比

### 核心設計原則

```
1. Worker Pool（工作池）
   問題：Goroutine 洪水（10,000 個同時）
   方案：固定數量 worker（10 個）
   效果：內存穩定、不被封禁

2. Bloom Filter（布隆過濾器）
   問題：map 內存爆炸（1.2 GB）
   方案：概率型數據結構（9 MB）
   效果：節省 99.2% 內存

3. robots.txt 解析
   問題：被網站封禁（不遵守規則）
   方案：解析並遵守 robots.txt
   效果：道德合規、不被封

4. URL Frontier（優先級隊列）
   問題：無關頁面優先爬（浪費時間）
   方案：按優先級排序（商品頁優先）
   效果：快速爬到重要頁面

5. DNS 緩存
   問題：DNS 查詢佔 45% 時間
   方案：緩存 DNS 結果（TTL 5 分鐘）
   效果：速度提升 47%
```

### 方案對比

| 方案 | 吞吐量 | 內存 | 合規性 | 適用規模 |
|------|-------|------|--------|---------|
| **單線程** | 1 URL/s | 50 MB | ✅ | < 10 萬 |
| **多線程（無限制）** | 100 URL/s | 10 GB ❌ | ❌ 被封 | 不適用 |
| **Worker Pool** | 10 URL/s | 200 MB | ✅ | < 100 萬 |
| **分布式（10 機器）** | 100 URL/s | 2 GB | ✅ | < 1 億 |
| **Google 規模** | 100 萬 URL/s | TB 級 | ✅ | 數千億 |

### 適用場景

**適合使用爬蟲的場景：**
- 價格監控（電商比價）
- 新聞聚合（RSS 替代）
- SEO 分析（競品研究）
- 數據採集（公開數據）

**不適合的場景：**
- 需要登錄的內容（違反 ToS）
- 個人信息（隱私問題）
- 動態生成內容（需要 Headless Browser）
- 有明確禁止的網站（robots.txt: Disallow: /）

### 關鍵指標

```
最終性能（Worker Pool + Bloom Filter + DNS Cache）：
- 支持 URL 數：1,000 萬
- 吞吐量：10 URL/秒（單機，遵守 Crawl-delay）
- 內存占用：約 200 MB
- DNS 緩存命中率：99%+

道德與合規：
- 100% 遵守 robots.txt ✅
- 設置清晰的 User-Agent ✅
- 遵守 Crawl-delay ✅
- 不爬取私人數據 ✅
```

### 延伸閱讀

**框架：**
- Scrapy（Python）- 最流行的爬蟲框架
- Colly（Go）- 輕量級爬蟲庫
- Puppeteer / Playwright（JavaScript 渲染）

**算法：**
- Bloom Filter（去重）
- SimHash（內容去重）
- PageRank（頁面重要性）
- Consistent Hashing（URL 分配）

**工業實踐：**
- Common Crawl（開源網頁快照）
- Internet Archive（網頁歸檔）
- Google Caffeine（實時索引）

---

從「融資路演的尷尬」到「每秒 10 個 URL 的穩定爬蟲」，Web Crawler 系統經歷了 7 次重大演進：

1. **單線程太慢** → Worker Pool
2. **Goroutine 洪水** → 限制並發
3. **內存爆炸** → Bloom Filter
4. **被封禁** → robots.txt + Politeness
5. **URL 混亂** → URL Frontier 優先級隊列
6. **DNS 瓶頸** → DNS 緩存
7. **擴展性** → 分布式架構

**記住：** 爬蟲不僅是技術問題，更是道德問題。尊重網站所有者的意願（robots.txt）、限制請求頻率（Politeness）、設置清晰的 User-Agent，是專業爬蟲的基本素養。像 Google 一樣，做一個「有禮貌的爬蟲」。
