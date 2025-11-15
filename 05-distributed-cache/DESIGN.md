# Distributed Cache 系統設計文檔

## 場景：你是電商平台的後端架構師

### 雙十一前夕的危機

距離雙十一還有兩週，運維團隊負責人急忙找到你：

> **運維：** "資料庫快撐不住了！昨天做壓測，PostgreSQL CPU 直接飆到 95%，慢查詢一堆，P99 延遲超過 3 秒！"

你查看壓測報告：

```
壓測數據（模擬雙十一 10 倍流量）：
- 商品查詢 QPS：15,000（正常 1,500）
- 資料庫 QPS：15,000（全部打到 DB）
- P50 延遲：250 ms
- P99 延遲：3,200 ms（不可接受！）
- 資料庫 CPU：95%
- 錯誤率：8%（timeout）

熱門商品查詢：
- 商品 ID 10001：被查詢 5,000 次/分鐘
- 商品 ID 10002：被查詢 4,800 次/分鐘
- 商品 ID 10003：被查詢 4,500 次/分鐘

問題：
同一個商品資料在 1 分鐘內被查詢數千次
每次都打資料庫，完全沒有復用！
```

你陷入思考：

- 如何減少資料庫負載？
- 如何提升查詢速度？
- 如何應對 10 倍流量？

### 你會問自己：

1. **能否不每次都查資料庫？**
   - 商品資料很少變化
   - 為何要重複查詢？

2. **記憶體能不能存下？**
   - 100 萬個商品 × 1 KB = 1 GB
   - 單機記憶體夠嗎？

3. **如何保證資料一致性？**
   - 商品價格更新後，快取怎麼辦？

---

## 第一次嘗試：加入本地記憶體快取

### 最直覺的想法

你想：「把查詢過的資料放在記憶體，下次直接返回！」

```go
type SimpleCache struct {
    data map[string]interface{}
    mu   sync.RWMutex
}

func (c *SimpleCache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    value, ok := c.data[key]
    return value, ok
}

func (c *SimpleCache) Set(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.data[key] = value
}

// 查詢商品
func GetProduct(productID string) (*Product, error) {
    // 1. 先查快取
    if value, ok := cache.Get(productID); ok {
        return value.(*Product), nil
    }

    // 2. 查資料庫
    product, err := db.QueryProduct(productID)
    if err != nil {
        return nil, err
    }

    // 3. 寫入快取
    cache.Set(productID, product)

    return product, nil
}
```

### 時序範例

```
第一次查詢商品 10001：
10:00:00 → 查快取 → 未命中
10:00:01 → 查資料庫 → 耗時 50 ms
10:00:05 → 返回商品資料
10:00:05 → 寫入快取

第二次查詢商品 10001（1 秒後）：
10:00:06 → 查快取 → 命中！
10:00:06 → 直接返回（耗時 < 1 ms）

第三次查詢商品 10001（10 秒後）：
10:00:16 → 查快取 → 命中！
10:00:16 → 直接返回（耗時 < 1 ms）

效果：
- 第 1 次：查資料庫（50 ms）
- 第 2-100 次：查快取（< 1 ms）
- 資料庫負載降低 99%！
```

你興奮地部署到測試環境，再次壓測。

### 災難場景：記憶體爆滿

壓測幾小時後，服務崩潰：

```
監控告警：
14:32:15 → Memory usage: 98%
14:32:20 → OOM Killer triggered
14:32:21 → Service crashed

錯誤日誌：
fatal error: out of memory
runtime: out of memory

分析：
- 快取了 500 萬個商品（遠超預期）
- 每個商品 1 KB
- 總記憶體：5 GB（超過伺服器 4 GB 限制）

問題：
快取無限增長，沒有淘汰機制！
```

**問題發現：記憶體洩漏（沒有淘汰策略）**

```
視覺化問題：

時間線：
T0: 快取 0 個商品 → 記憶體 0 MB
T1: 快取 10 萬個 → 記憶體 100 MB
T2: 快取 100 萬個 → 記憶體 1 GB
T3: 快取 500 萬個 → 記憶體 5 GB → 崩潰！

原因：
- 所有查詢過的資料都存在快取
- 永不刪除
- 記憶體無限增長

實際情況：
- 熱門商品只有 1 萬個（1% 的商品）
- 這 1 萬個占了 99% 的流量
- 其他 499 萬個是冷門商品，很少被訪問
- 但我們快取了所有商品！
```

### 你會問自己：

1. **如何限制快取大小？**
   - 最多存多少個？
   - 超過時怎麼辦？

2. **應該淘汰哪些資料？**
   - 最舊的？
   - 最少用的？
   - 隨機的？

---

## 第二次嘗試：FIFO 淘汰策略

### 新的想法

你想：「限制快取大小，滿了就淘汰最舊的！」

```go
type FIFOCache struct {
    capacity int
    data     map[string]interface{}
    queue    []string  // 記錄加入順序
    mu       sync.RWMutex
}

func (c *FIFOCache) Set(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()

    // 如果已存在，更新
    if _, exists := c.data[key]; exists {
        c.data[key] = value
        return
    }

    // 檢查容量
    if len(c.data) >= c.capacity {
        // 淘汰最舊的（queue 第一個）
        oldest := c.queue[0]
        delete(c.data, oldest)
        c.queue = c.queue[1:]
    }

    // 加入新資料
    c.data[key] = value
    c.queue = append(c.queue, key)
}
```

### 時序範例

```
場景：快取容量 = 3，查詢順序 A → B → C → D

T1: 查詢 A
→ 快取未命中，查資料庫
→ 快取 = [A]

T2: 查詢 B
→ 快取未命中，查資料庫
→ 快取 = [A, B]

T3: 查詢 C
→ 快取未命中，查資料庫
→ 快取 = [A, B, C]（滿了）

T4: 查詢 A（熱門商品，再次查詢）
→ 快取命中 ✓
→ 快取 = [A, B, C]

T5: 查詢 D
→ 快取未命中，查資料庫
→ 淘汰 A（最舊的）
→ 快取 = [B, C, D]

T6: 查詢 A（又來了！）
→ 快取未命中（剛被淘汰！）
→ 又要查資料庫...

問題：熱門商品 A 被淘汰了！
```

### 災難場景：命中率極低

雙十一當天，監控數據顯示：

```
快取效能（實際流量）：
- 總查詢：1,000 萬次
- 快取命中：200 萬次（20%）
- 快取未命中：800 萬次（80%）
- 命中率：20%（太低！）

分析：
熱門商品（TOP 100）：
- 占 80% 的流量
- 但不斷被冷門商品擠出快取

冷門商品（99.9%）：
- 只占 20% 流量
- 但占據大部分快取空間

結論：FIFO 無法保護熱門資料！
```

**問題發現：忽略訪問頻率**

```
問題本質：
FIFO 只看「加入時間」，不看「訪問頻率」

視覺化：
商品 A：被訪問 10,000 次（熱門）
商品 D：被訪問 1 次（冷門）

FIFO 的選擇：淘汰 A（因為它最舊）
正確選擇應該是：淘汰 D（因為它最少用）
```

### 你會問自己：

1. **如何識別熱門資料？**
   - 記錄訪問次數？
   - 記錄最後訪問時間？

2. **什麼策略最好？**
   - 淘汰最久未用的（LRU）？
   - 淘汰最少用的（LFU）？

---

## 靈感：作業系統的分頁置換

你想起大學作業系統課程學過的 LRU (Least Recently Used)：

```
作業系統記憶體管理：
- 實體記憶體有限
- 分頁（Page）需要置換
- LRU 算法：淘汰最久未使用的分頁

原理：
如果一個分頁最近被訪問，短期內很可能再次被訪問
這就是「時間局部性原理」（Temporal Locality）

應用到快取：
- 最近查詢的商品，短期內可能再次查詢
- 應該保留在快取中
- 淘汰最久未訪問的
```

**關鍵洞察：**
- 不看「加入時間」，看「最後訪問時間」
- 最近用過的 = 可能再用 = 保留
- 很久沒用的 = 可能不用 = 淘汰

這就是 **LRU（Least Recently Used）算法**！

---

## 第三次嘗試：LRU 淘汰策略

### 設計思路

```
資料結構：
1. HashMap：key → 節點（O(1) 查找）
2. 雙向鏈表：維護訪問順序
   - 頭部：最近訪問
   - 尾部：最久未訪問

操作：
- Get(key)：
  1. 查找 HashMap
  2. 移動節點到鏈表頭部（標記為最近使用）

- Put(key, value)：
  1. 如果已存在：更新 + 移到頭部
  2. 如果不存在：
     - 容量未滿：插入頭部
     - 容量已滿：淘汰尾部 + 插入頭部

時間複雜度：所有操作 O(1)
```

### 實現

```go
type LRUCache struct {
    capacity int
    cache    map[string]*list.Element
    list     *list.List
    mu       sync.RWMutex
}

type entry struct {
    key   string
    value interface{}
}

func NewLRU(capacity int) *LRUCache {
    return &LRUCache{
        capacity: capacity,
        cache:    make(map[string]*list.Element),
        list:     list.New(),
    }
}

func (c *LRUCache) Get(key string) (interface{}, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if elem, ok := c.cache[key]; ok {
        // 移到鏈表頭部（標記為最近使用）
        c.list.MoveToFront(elem)
        return elem.Value.(*entry).value, true
    }
    return nil, false
}

func (c *LRUCache) Set(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()

    // 如果已存在，更新並移到頭部
    if elem, ok := c.cache[key]; ok {
        c.list.MoveToFront(elem)
        elem.Value.(*entry).value = value
        return
    }

    // 檢查容量
    if c.list.Len() >= c.capacity {
        // 淘汰尾部（最久未使用）
        back := c.list.Back()
        if back != nil {
            c.list.Remove(back)
            delete(c.cache, back.Value.(*entry).key)
        }
    }

    // 插入頭部
    e := &entry{key, value}
    elem := c.list.PushFront(e)
    c.cache[key] = elem
}
```

### 時序範例

```
場景：快取容量 = 3，熱門商品 A 被頻繁訪問

T1: 查詢 A
→ 快取 = [A]（A 在頭部）

T2: 查詢 B
→ 快取 = [B, A]

T3: 查詢 C
→ 快取 = [C, B, A]（滿了）

T4: 查詢 A（熱門商品）
→ A 移到頭部
→ 快取 = [A, C, B]

T5: 查詢 A（又來了）
→ A 已在頭部
→ 快取 = [A, C, B]

T6: 查詢 D（冷門商品）
→ 淘汰 B（尾部，最久未用）
→ 快取 = [D, A, C]

T7: 查詢 A（又來了）
→ A 移到頭部
→ 快取 = [A, D, C]

結果：熱門商品 A 一直保留在快取中！
```

### 效能提升

```
雙十一壓測結果：

LRU vs FIFO：
- FIFO 命中率：20%
- LRU 命中率：82%（提升 4 倍！）

資料庫負載：
- 原本：15,000 QPS
- FIFO：12,000 QPS（降低 20%）
- LRU：2,700 QPS（降低 82%！）

延遲改善：
- P50：250 ms → 5 ms
- P99：3,200 ms → 50 ms

結論：LRU 大幅提升命中率，保護熱門資料！
```

### 為什麼這是最佳選擇？

對比所有淘汰策略：

| 特性 | FIFO | Random | LRU | LFU |
|------|------|--------|-----|-----|
| 時間複雜度 | O(1) | O(1) | O(1) | O(1) |
| 空間複雜度 | O(N) | O(N) | O(N) | O(N) |
| 命中率（熱門資料） | 低 | 低 | 高 | 最高 |
| 實現難度 | 簡單 | 簡單 | 中等 | 複雜 |
| 快取污染防護 | 無 | 無 | 弱 | 強 |
| 適用場景 | 無 | 無 | 通用 | 穩定訪問模式 |

**LRU 勝出原因：**
1. 時間複雜度 O(1)，效能好
2. 保護熱門資料（時間局部性）
3. 實現相對簡單（HashMap + 雙向鏈表）
4. 適合大部分場景

---

## 新挑戰：單機容量不足

### 場景升級

雙十一第二天，商品團隊找到你：

> **商品團隊：** "我們要擴充商品目錄，從 100 萬增加到 1,000 萬個商品。快取能支援嗎？"

你計算容量需求：

```
容量估算：
- 商品總數：1,000 萬個
- 熱門商品（20%）：200 萬個
- 平均大小：1 KB
- 需要快取容量：200 萬 × 1 KB = 2 GB

當前配置：
- 單機記憶體：4 GB
- 作業系統：1 GB
- 應用程式：1 GB
- 可用於快取：2 GB

問題：剛好夠用，但沒有餘裕！
```

一週後，商品擴充到 2,000 萬個：

```
新需求：
- 商品總數：2,000 萬個
- 熱門商品（20%）：400 萬個
- 需要容量：400 萬 × 1 KB = 4 GB

問題：單機記憶體不夠了！
```

### 第一次想法：垂直擴展（升級機器）

```
方案：
- 當前機器：4 GB 記憶體
- 升級到：16 GB 記憶體
- 成本：$200/月 → $500/月

問題：
1. 有上限：單機最多 128 GB
2. 成本高：記憶體越大越貴
3. 無法水平擴展：不能加機器分擔負載
4. 單點故障：機器掛了整個快取就掛了
```

### 解決方案：分散式快取

你意識到：

> "需要多台機器分擔負載，水平擴展！"

```
分散式架構：
- 3 台快取伺服器，每台 16 GB
- 總容量：48 GB
- 每台處理 1/3 的請求

問題：如何分配資料到不同伺服器？
```

---

## 第四次嘗試：Hash 取模分片

### 簡單的分片策略

```go
func getServer(key string) int {
    hash := crc32.ChecksumIEEE([]byte(key))
    return int(hash % 3)  // 3 台伺服器
}

func Get(key string) (interface{}, error) {
    serverID := getServer(key)
    server := servers[serverID]

    return server.Get(key)
}
```

### 時序範例

```
3 台伺服器：server0, server1, server2

商品 A：hash(A) % 3 = 0 → server0
商品 B：hash(B) % 3 = 1 → server1
商品 C：hash(C) % 3 = 2 → server2
商品 D：hash(D) % 3 = 0 → server0

負載分佈：
- server0：A, D
- server1：B
- server2：C

看起來很均勻！
```

### 災難場景：擴容導致大量快取失效

三個月後，需要擴容到 4 台伺服器：

```
擴容前（3 台）：
- 商品 A：hash(A) % 3 = 0 → server0
- 商品 B：hash(B) % 3 = 1 → server1
- 商品 C：hash(C) % 3 = 2 → server2

擴容後（4 台）：
- 商品 A：hash(A) % 4 = ? → 可能改變！
- 商品 B：hash(B) % 4 = ? → 可能改變！
- 商品 C：hash(C) % 4 = ? → 可能改變！

實際影響：
- 100 萬個快取的商品
- 約 75% 需要重新分配（75 萬個）
- 擴容瞬間命中率降到 25%
- 資料庫壓力暴增 3 倍！
```

**問題發現：大量資料重新分配**

```
計算：
舊：hash % 3
新：hash % 4

只有 hash % 3 == hash % 4 的資料不需遷移
大約 25% 資料位置不變
75% 需要重新分配

視覺化：
擴容前：[A, B, C] 分佈在 3 台
擴容後：[A, B, C, D] 需要重新分佈
→ A 可能從 server0 移到 server2
→ B 可能從 server1 移到 server3
→ 大量快取失效！
```

### 你會問自己：

1. **如何減少擴容影響？**
   - 能否只遷移部分資料？
   - 增加 1 台，只遷移 1/4 的資料？

2. **有沒有更好的分片算法？**
   - 不依賴伺服器總數？

---

## 靈感：一致性雜湊（Consistent Hashing）

你查閱論文，發現一致性雜湊算法：

```
傳統雜湊的問題：
hash(key) % N
→ N 改變時，幾乎所有 key 重新分配

一致性雜湊的做法：
1. 將雜湊空間視為環（0 到 2^32-1）
2. 伺服器映射到環上某個位置
3. 資料映射到環上，順時針找最近的伺服器

優勢：
- 增加伺服器：只影響相鄰區間
- 刪除伺服器：只影響該伺服器負責的資料
- 理論上只需遷移 K/N 的資料（K=總資料，N=總伺服器數）
```

**關鍵洞察：**
- 傳統雜湊：依賴總數 N，N 變化影響全部
- 一致性雜湊：與總數無關，變化只影響局部
- 用「環」的概念消除對總數的依賴

這就是 **一致性雜湊（Consistent Hashing）**！

---

## 最終方案：一致性雜湊 + 虛擬節點

### 設計思路

```
1. 雜湊環：
   - 範圍：0 到 2^32-1
   - 首尾相連形成環

2. 伺服器映射：
   - hash(server1) = 100
   - hash(server2) = 200
   - hash(server3) = 300

3. 資料映射：
   - hash(keyA) = 50
   - 順時針找最近的伺服器 → server1 (100)

4. 虛擬節點：
   - 問題：3 台伺服器在環上分佈可能不均
   - 解決：每台伺服器對應多個虛擬節點
   - 例如：server1 對應 server1-0, server1-1, ..., server1-149
```

### 實現

```go
type ConsistentHash struct {
    hashFunc    func([]byte) uint32
    replicas    int                    // 虛擬節點數（如 150）
    ring        []uint32               // 雜湊環（排序）
    hashMap     map[uint32]string      // 雜湊值 → 節點名
    nodes       map[string]bool
    mu          sync.RWMutex
}

func NewConsistentHash(replicas int) *ConsistentHash {
    return &ConsistentHash{
        hashFunc: crc32.ChecksumIEEE,
        replicas: replicas,
        hashMap:  make(map[uint32]string),
        nodes:    make(map[string]bool),
    }
}

func (ch *ConsistentHash) Add(node string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()

    for i := 0; i < ch.replicas; i++ {
        // 創建虛擬節點
        virtualNode := fmt.Sprintf("%s#%d", node, i)
        hash := ch.hashFunc([]byte(virtualNode))

        ch.ring = append(ch.ring, hash)
        ch.hashMap[hash] = node
    }

    // 排序環
    sort.Slice(ch.ring, func(i, j int) bool {
        return ch.ring[i] < ch.ring[j]
    })

    ch.nodes[node] = true
}

func (ch *ConsistentHash) Get(key string) string {
    ch.mu.RLock()
    defer ch.mu.RUnlock()

    if len(ch.ring) == 0 {
        return ""
    }

    hash := ch.hashFunc([]byte(key))

    // 二分搜尋找到順時針最近的節點
    idx := sort.Search(len(ch.ring), func(i int) bool {
        return ch.ring[i] >= hash
    })

    if idx == len(ch.ring) {
        idx = 0  // 環形，回到開頭
    }

    return ch.hashMap[ch.ring[idx]]
}

func (ch *ConsistentHash) Remove(node string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()

    for i := 0; i < ch.replicas; i++ {
        virtualNode := fmt.Sprintf("%s#%d", node, i)
        hash := ch.hashFunc([]byte(virtualNode))

        // 從環中移除
        idx := sort.Search(len(ch.ring), func(i int) bool {
            return ch.ring[i] >= hash
        })
        if idx < len(ch.ring) && ch.ring[idx] == hash {
            ch.ring = append(ch.ring[:idx], ch.ring[idx+1:]...)
        }

        delete(ch.hashMap, hash)
    }

    delete(ch.nodes, node)
}
```

### 時序範例

```
場景：3 台伺服器擴容到 4 台

擴容前（3 台，每台 150 個虛擬節點）：
- 總虛擬節點：450 個
- 商品 A 映射到：server1
- 商品 B 映射到：server2
- 商品 C 映射到：server3

擴容（增加 server4）：
- 新增 150 個虛擬節點
- 總虛擬節點：600 個

影響分析：
- server4 分擔約 1/4 的資料
- 從 server1, server2, server3 各拿一部分
- 商品 A：仍在 server1（約 75% 機率）
- 商品 B：可能移到 server4（約 25% 機率）

遷移量計算：
- 100 萬個商品
- 只需遷移：100 萬 × 1/4 = 25 萬個
- 相比 hash % N：75 萬個
- 減少遷移：67%！
```

### 虛擬節點的作用

```
問題：物理節點少時負載不均

3 台伺服器，無虛擬節點：
Ring: [server1(位置100), server2(位置200), server3(位置300)]
→ 可能 server1 負責 0-100 (100 個單位)
→ server2 負責 100-200 (100 個單位)
→ server3 負責 200-2^32 (極大範圍！)
→ 負載極度不均！

3 台伺服器，每台 150 個虛擬節點：
Ring: [server1#0, server2#5, server1#1, server3#2, ...]（450個節點混合）
→ 450 個虛擬節點均勻分佈
→ 每台物理伺服器約負責 1/3 的資料
→ 負載均衡！

實測：
- 虛擬節點數 = 150
- 3 台伺服器負載分佈：33.2%, 33.5%, 33.3%
- 標準差 < 1%
```

---

## 新挑戰：快取穿透攻擊

### 災難場景

雙十一凌晨 3 點，安全團隊發現異常：

```
監控告警：
03:00:00 → DB QPS 突然暴增至 50,000
03:00:05 → DB CPU: 98%
03:00:10 → 大量查詢 timeout

安全日誌：
03:00:00.123 GET /product/99999999 → 404 (DB查詢)
03:00:00.145 GET /product/99999998 → 404 (DB查詢)
03:00:00.167 GET /product/99999997 → 404 (DB查詢)
...
03:00:10.000 累計 100,000 次查詢不存在的商品

攻擊分析：
- 所有商品 ID 都不存在
- 快取未命中 → 每次都查資料庫
- 資料庫返回空 → 不會寫快取
- 下次還是未命中 → 又查資料庫
- 惡意循環！
```

**問題發現：快取穿透（Cache Penetration）**

```
問題本質：
查詢不存在的資料 → 快取無效 → 每次打資料庫

攻擊向量：
for i in range(1000000):
    requests.get(f"/product/{random_invalid_id()}")

效果：
- 每個請求都穿透快取
- 直接打到資料庫
- 資料庫壓力暴增
```

### 解決方案：Bloom Filter 預檢

```
Bloom Filter 原理：
- 一個 bit 陣列 + 多個雜湊函數
- 檢查元素是否「可能存在」
- 如果返回「不存在」→ 100% 確定不存在
- 如果返回「可能存在」→ 需要進一步查詢

空間效率：
- 1,000 萬個商品
- 誤判率 1%
- 只需 11.5 MB 記憶體！

流程：
1. 啟動時：將所有商品 ID 加入 Bloom Filter
2. 查詢時：先檢查 Bloom Filter
   - 不存在 → 直接返回 404（不查資料庫）
   - 可能存在 → 查快取/資料庫
```

### 實現（概念）

```go
// 生產環境建議使用 Redis Bloom Filter 模組
func GetProductWithBloomFilter(productID string) (*Product, error) {
    // 1. Bloom Filter 預檢
    if !bloomFilter.MayContain(productID) {
        // 確定不存在，直接返回
        return nil, ErrNotFound
    }

    // 2. 查快取
    if product, ok := cache.Get(productID); ok {
        return product.(*Product), nil
    }

    // 3. 查資料庫
    product, err := db.QueryProduct(productID)
    if err != nil {
        if err == sql.ErrNoRows {
            // 誤判（1% 機率）
            return nil, ErrNotFound
        }
        return nil, err
    }

    // 4. 寫快取
    cache.Set(productID, product)
    return product, nil
}
```

### 效果對比

```
場景：攻擊者發送 100,000 個不存在的商品 ID

不使用 Bloom Filter：
- 100,000 次快取查詢（全部未命中）
- 100,000 次資料庫查詢
- 資料庫壓力爆炸

使用 Bloom Filter：
- 100,000 次 Bloom Filter 檢查（記憶體，微秒級）
- 1,000 次資料庫查詢（1% 誤判）
- 資料庫負載降低 99%！
```

---

## 新挑戰：快取雪崩

### 災難場景

促銷活動結束後：

```
場景：
- 晚上 8 點開始促銷
- 批量載入 10 萬個商品到快取
- TTL = 2 小時
- 晚上 10 點促銷結束

災難時刻（晚上 10:00）：
10:00:00 → 10 萬個快取同時過期
10:00:01 → 10 萬個請求同時打到資料庫
10:00:02 → 資料庫 CPU: 100%
10:00:03 → 資料庫連接池耗盡
10:00:05 → 服務崩潰

監控數據：
- 快取命中率：從 85% 突降至 0%
- DB QPS：從 3,000 暴增至 50,000
- P99 延遲：從 10ms 暴增至 10,000ms
```

**問題發現：快取雪崩（Cache Avalanche）**

```
問題本質：
大量快取同時過期 → 瞬間所有請求打資料庫

視覺化：
時間軸：
20:00 → 10 萬個快取寫入（TTL=2h）
22:00 → 10 萬個快取同時過期
22:00:01 → 災難！
```

### 解決方案：隨機 TTL

```go
func SetWithRandomTTL(key string, value interface{}, baseTTL time.Duration) {
    // TTL = 基準時間 ± 隨機偏移
    // 例如：2小時 ± 10分鐘
    offset := time.Duration(rand.Intn(600)) * time.Second  // 0-600秒
    ttl := baseTTL + offset

    cache.SetWithTTL(key, value, ttl)
}

// 使用範例
SetWithRandomTTL("product:1001", product, 2*time.Hour)
// 實際 TTL：2h ~ 2h10m（隨機）
```

### 效果

```
改進前：
20:00:00 → 寫入 100,000 個快取（TTL=2h）
22:00:00 → 同時過期 100,000 個
22:00:01 → 資料庫崩潰

改進後：
20:00:00 → 寫入 100,000 個快取（TTL=2h±10min）
22:00:00 → 過期約 10,000 個（10%）
22:01:00 → 過期約 10,000 個（10%）
...
22:09:00 → 過期約 10,000 個（10%）

結果：
- 過期時間分散到 10 分鐘內
- 每分鐘只有 10,000 個過期（資料庫可承受）
- 避免瞬間壓力
```

---

## 擴展性分析

### 當前架構容量

```
分散式快取（3 節點）：
├─ 每節點：16 GB 記憶體
├─ 總容量：48 GB
├─ 演算法：LRU
├─ 分片：一致性雜湊（150 虛擬節點/物理節點）
└─ 性能：每節點 50,000 QPS

總性能：
- 讀取 QPS：150,000
- 寫入 QPS：50,000
- 命中率：85%

適用場景：
- 商品數：< 5,000 萬個
- 日活：< 1,000 萬
- 成本：約 $600/月
```

### 10 倍擴展（1,500,000 讀取 QPS）

```
方案：增加到 30 個節點

架構：
├─ 30 個快取節點（每個 16 GB）
├─ 總容量：480 GB
├─ 一致性雜湊自動負載均衡
└─ 每節點：50,000 QPS

性能：
- 讀取 QPS：1,500,000
- 寫入 QPS：500,000

擴容過程：
- 一次增加 27 個節點
- 資料自動重新分佈（一致性雜湊）
- 遷移量：約 43 GB（90%）
- 遷移時間：約 10 分鐘

成本：
- 每節點：$100/月（AWS r5.large）
- 30 節點：$3,000/月
```

### 100 倍擴展（15,000,000 讀取 QPS）

```
需要架構升級：

1. 多層快取
   L1：本地快取（應用內）
   - 容量：每實例 1 GB
   - 命中率：50%
   - 延遲：< 100 µs
   - 演算法：LRU

   L2：分散式快取（Redis Cluster）
   - 容量：300 節點 × 16 GB = 4.8 TB
   - 命中率：40%（L1 未命中的）
   - 延遲：< 2 ms
   - 分片：一致性雜湊

   L3：資料庫
   - 命中率：10%（L1+L2 未命中）
   - 延遲：~20 ms

   總效果：
   - 綜合命中率：90%
   - 平均延遲：50% × 0.1ms + 40% × 2ms + 10% × 20ms = 2.85ms

2. 副本策略（高可用）
   - 3 副本（1 主 + 2 從）
   - 讀取可從任意副本
   - 寫入主副本，非同步同步到從副本
   - 自動故障轉移

成本估算：
- 快取節點：300 × $100 = $30,000/月
- 負載平衡：$500/月
- 監控：$1,000/月
- 總計：約 $31,500/月
```

---

## 真實工業案例

### Redis（最流行的快取系統）

```
技術選型：
- 淘汰策略：8 種（包括 LRU、LFU）
- 資料結構：String、Hash、List、Set、ZSet
- 持久化：RDB 快照 + AOF 日誌

特點：
- 單執行緒模型（簡化並發）
- 高效能：100,000+ QPS
- 支援主從複製、Sentinel、Cluster

使用者：
- Twitter：時間線快取
- Instagram：用戶資料快取
- Stack Overflow：頁面快取
```

### Memcached

```
技術選型：
- 淘汰策略：LRU
- 簡單 Key-Value 儲存
- 無持久化（純記憶體）

特點：
- 多執行緒模型（利用多核）
- 簡單高效
- Slab 記憶體管理（減少碎片）

使用者：
- Facebook：User Session
- Wikipedia：頁面快取
```

### Caffeine（Java 高效能快取）

```
技術選型：
- 淘汰策略：W-TinyLFU（Window TinyLFU）
- 本地快取（應用內）
- 自動載入、過期、統計

特點：
- 命中率極高（優於 Guava Cache）
- 近乎最佳的淘汰策略
- 低 GC 開銷

為什麼選擇：
- Java 生態最佳本地快取
- Benchmark 顯示比其他快取快 3-5 倍
```

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **LRU 算法** | `lru.go:18-135` | HashMap + 雙向鏈表，O(1) 操作 |
| **LFU 算法** | `lfu.go:45-214` | 頻率桶，防快取污染 |
| **一致性雜湊** | `consistent.go` | 虛擬節點，減少遷移 |
| **Cache-Aside** | `aside.go` | 旁路快取模式 |
| **並發安全** | 各演算法 | sync.RWMutex 讀寫鎖 |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **Bloom Filter** | 增加複雜度，聚焦核心演算法 | Redis Bloom 模組 |
| **TTL 管理** | 聚焦淘汰策略 | 定時清理過期資料 |
| **持久化** | 純記憶體快取示範 | RDB + AOF |
| **主從複製** | 單機示範 | 3 副本高可用 |

### 生產環境額外需要

```
1. 高可用性
   - 主從複製：1 主 + 2 從
   - 自動故障轉移：Sentinel/Raft
   - 健康檢查：心跳檢測

2. 持久化
   - RDB 快照：定期全量備份
   - AOF 日誌：操作日誌
   - 混合持久化

3. 監控告警
   - 命中率：目標 > 80%
   - 延遲：P50/P95/P99
   - 記憶體使用：告警 > 80%
   - 淘汰率：過高表示容量不足

4. 安全性
   - 認證：密碼/Token
   - 加密：TLS
   - 隔離：多租戶
```

---

## 你學到了什麼？

### 1. 從錯誤中學習

```
錯誤方案的價值：

方案 A：無限快取
發現：記憶體爆滿，OOM
教訓：必須有淘汰機制

方案 B：FIFO 淘汰
發現：命中率只有 20%
教訓：需要考慮訪問模式

方案 C：LRU
成功：命中率 82%，保護熱門資料
教訓：時間局部性原理很重要

方案 D：Hash 取模分片
發現：擴容時 75% 資料需遷移
教訓：需要更好的分片算法

方案 E：一致性雜湊
成功：擴容只需遷移 25% 資料
教訓：演算法設計影響擴展性
```

### 2. 完美方案不存在

```
所有淘汰策略都有權衡：

LRU：
優勢：簡單高效，適合通用場景
劣勢：快取污染問題

LFU：
優勢：防污染，保護高頻資料
劣勢：實現複雜，冷啟動問題

FIFO：
優勢：實現最簡單
劣勢：命中率低

教訓：根據訪問模式選擇
```

### 3. 真實場景驅動設計

```
問題演進：

第一階段：資料庫壓力大
→ 需求：加快查詢速度
→ 方案：本地快取

第二階段：記憶體爆滿
→ 需求：限制快取大小
→ 方案：淘汰策略（LRU）

第三階段：單機容量不足
→ 需求：水平擴展
→ 方案：分散式快取 + 一致性雜湊

第四階段：遭受攻擊
→ 需求：防止快取穿透
→ 方案：Bloom Filter

教訓：系統設計是持續演進的
```

### 4. 工業界如何選擇

| 場景 | 推薦方案 | 原因 |
|------|---------|------|
| **通用場景** | Redis (LRU/LFU) | 成熟穩定，功能豐富 |
| **高效能** | Memcached | 多執行緒，吞吐高 |
| **Java 本地快取** | Caffeine | 命中率最高 |
| **大規模** | Redis Cluster | 支援分片，易擴展 |

---

## 總結

Distributed Cache 展示了**高效能快取系統**的設計演進：

1. **發現問題**：資料庫無法承受高 QPS
2. **本地快取**：記憶體爆滿需要淘汰策略
3. **LRU 演算法**：O(1) 操作，保護熱門資料
4. **分散式擴展**：一致性雜湊減少遷移
5. **防護攻擊**：Bloom Filter 防穿透，隨機 TTL 防雪崩

**核心思想：** 用淘汰策略控制記憶體，用一致性雜湊實現擴展，用多層防護保證穩定。

**適用場景：**
- 資料庫查詢快取
- API 回應快取
- 會話儲存
- 熱點資料加速

**不適用：**
- 強一致性需求（金融交易）
- 資料量極小（不值得快取）
- 資料變化極快（快取失效過快）

**關鍵權衡：**
- 命中率 vs 實現複雜度（LRU vs LFU）
- 一致性 vs 效能（同步 vs 非同步）
- 記憶體 vs 準確性（Bloom Filter 誤判率）
