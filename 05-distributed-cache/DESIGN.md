# Distributed Cache 系統設計文檔

## 📋 問題定義

### 業務需求
構建分布式快取系統，提升應用性能：
- **減少資料庫負載**：快取熱門數據，減少 DB 查詢
- **降低延遲**：內存讀取 < 1ms vs 資料庫 ~10ms
- **提升吞吐量**：支持更高的 QPS
- **水平擴展**：單機容量有限，需要分布式擴展
- **高可用性**：節點故障不影響服務

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **單機 QPS** | 100K (讀取) | 如何實現 O(1) 操作？ |
| **讀取延遲** | < 1ms (本地) | 如何選擇數據結構？ |
| **分布式延遲** | < 5ms (遠程) | 如何減少網絡開銷？ |
| **快取命中率** | > 80% | 如何選擇淘汰策略？ |
| **容量** | 100 GB+ (分布式) | 如何水平擴展？ |

### 容量估算
```
假設：
- 日活用戶：1000 萬
- 每用戶查詢：100 次/天
- 快取命中率：80%
- 平均對象大小：1 KB

計算：
- 總查詢：1000 萬 × 100 = 10 億/天 ≈ 11,600 QPS
- 快取查詢：11,600 × 80% = 9,280 QPS
- DB 查詢：11,600 × 20% = 2,320 QPS
- 快取容量（20% 熱數據）：1000 萬 × 0.2 × 1KB = 2 GB
```

---

## 🤔 設計決策樹

### 決策 1：選擇哪種淘汰算法？

```
問題：快取容量有限，當滿時應該淘汰哪些數據？

❌ 方案 A：FIFO（First In First Out）
   機制：淘汰最早加入的數據

   問題：
   - 忽略訪問頻率：熱門數據可能被淘汰
   - 命中率低

   範例：
   - 加入順序：A → B → C → D
   - 容量滿，加入 E
   - 淘汰 A（即使 A 是熱門數據）❌

❌ 方案 B：Random（隨機淘汰）
   問題：
   - 無法保護熱門數據
   - 命中率不穩定

✅ 方案 C：LRU（Least Recently Used）⭐
   機制：淘汰最久未使用的數據

   數據結構：
   - HashMap：key → 節點（O(1) 查找）
   - 雙向鏈表：維護訪問順序

   操作：
   - Get(key)：查找 → 移到鏈表頭部
   - Put(key, value)：插入頭部 / 更新 → 移到頭部
   - 淘汰：移除鏈表尾部（最久未使用）

   時間複雜度：O(1)

   優勢：
   - 實現簡單：HashMap + 鏈表
   - 性能好：所有操作 O(1)
   - 適合大部分場景：時間局部性

   問題：
   - 快取污染：一次性大量訪問會淘汰熱門數據
   - 無法區分訪問頻率：訪問 1 次和 1000 次無區別

   範例（快取污染）：
   - 熱門數據：A(1000次), B(1000次), C(1000次)
   - 突然訪問：D, E, F, G（各 1 次）
   - 如果容量只有 4，熱門數據 A,B,C 被淘汰❌

✅ 方案 D：LFU（Least Frequently Used）⭐
   機制：淘汰訪問頻率最低的數據

   數據結構：
   - HashMap：key → 節點資訊（value, 頻率）
   - 頻率桶：每個頻率對應一個 LRU 鏈表
   - minFreq：追蹤最小頻率

   操作：
   - Get(key)：查找 → 頻率 +1 → 移到新頻率桶
   - Put(key, value)：插入到頻率=1 的桶
   - 淘汰：移除 minFreq 桶的鏈表尾部

   時間複雜度：O(1)

   優勢：
   - 防止快取污染：一次性訪問不會淘汰熱門數據
   - 保護熱點數據：高頻訪問的數據優先保留
   - 適合穩定訪問模式

   問題：
   - 實現複雜：多層數據結構
   - 冷啟動問題：新數據頻率低，容易被淘汰
   - 空間開銷大：需存儲頻率信息

   範例（防止污染）：
   - 熱門數據：A(freq=1000), B(freq=1000), C(freq=1000)
   - 突然訪問：D(freq=1), E(freq=1), F(freq=1), G(freq=1)
   - 淘汰：D,E,F,G（頻率低）✅
   - 保護：A,B,C（頻率高）✅

方案 E：LRU-K（考慮 K 次訪問）
   機制：考慮最近 K 次訪問的時間
   問題：實現複雜，K 難以確定

方案 F：2Q（Two Queues）
   機制：
   - FIFO 隊列：首次訪問
   - LRU 隊列：二次訪問
   優勢：防止快取污染
   權衡：實現複雜度 vs LFU
```

**選擇：LRU（通用場景）+ LFU（防快取污染場景）**

**實現細節：**
```go
// LRU 實現
type LRU struct {
    capacity int
    cache    map[string]*list.Element
    list     *list.List  // 雙向鏈表
}

func (lru *LRU) Get(key string) (interface{}, bool) {
    if elem, ok := lru.cache[key]; ok {
        lru.list.MoveToFront(elem)  // 移到頭部
        return elem.Value, true
    }
    return nil, false
}

func (lru *LRU) Put(key string, value interface{}) {
    if elem, ok := lru.cache[key]; ok {
        lru.list.MoveToFront(elem)  // 更新
        elem.Value = value
        return
    }

    if lru.list.Len() >= lru.capacity {
        // 淘汰尾部
        back := lru.list.Back()
        lru.list.Remove(back)
        delete(lru.cache, back.Key)
    }

    // 插入頭部
    elem := lru.list.PushFront(&entry{key, value})
    lru.cache[key] = elem
}

// LFU 實現
type LFU struct {
    cache   map[string]*lfuNode
    freqMap map[int]*list.List  // 頻率 → LRU 鏈表
    minFreq int
}

func (lfu *LFU) Get(key string) (interface{}, bool) {
    if node, ok := lfu.cache[key]; ok {
        lfu.increaseFreq(node)  // 頻率 +1
        return node.value, true
    }
    return nil, false
}

func (lfu *LFU) increaseFreq(node *lfuNode) {
    oldFreq := node.freq
    // 從舊頻率鏈表移除
    lfu.freqMap[oldFreq].Remove(node.elem)

    // 頻率 +1
    node.freq++

    // 加入新頻率鏈表
    if lfu.freqMap[node.freq] == nil {
        lfu.freqMap[node.freq] = list.New()
    }
    node.elem = lfu.freqMap[node.freq].PushFront(node)

    // 更新 minFreq
    if lfu.freqMap[oldFreq].Len() == 0 && lfu.minFreq == oldFreq {
        lfu.minFreq++
    }
}
```

---

### 決策 2：如何實現分布式快取？

```
問題：單機內存有限（假設 16 GB），如何擴展到 100 GB+？

❌ 方案 A：傳統哈希取模（hash(key) % N）
   機制：key hash 後對節點數取模

   範例：3 個節點
   - key1 → hash(key1) % 3 = 1 → node1
   - key2 → hash(key2) % 3 = 2 → node2
   - key3 → hash(key3) % 3 = 0 → node0

   問題：節點增減時大量重新分配
   - 增加節點：3 → 4
     - key1: hash % 3 = 1 → hash % 4 = ? （可能改變）
     - 最壞情況：75% 數據需要重新分配❌

   計算：
   - 3 節點 → 4 節點
   - 只有 hash % 3 == hash % 4 的 key 不需遷移
   - 需要遷移：~75%
   - 1000 萬 key × 1KB × 75% = 7.5 GB 數據遷移

✅ 方案 B：一致性哈希（Consistent Hashing）⭐
   機制：
   - 哈希環：0 到 2^32-1
   - 節點映射到環上：hash(node) → 位置
   - 數據映射：hash(key) → 順時針找最近節點

   虛擬節點：
   - 問題：物理節點少時分布不均
   - 解決：每個物理節點對應多個虛擬節點（如 150 個）

   範例：
   - 物理節點：node1, node2, node3
   - 虛擬節點：
     - node1-0, node1-1, ..., node1-149
     - node2-0, node2-1, ..., node2-149
     - node3-0, node3-1, ..., node3-149
   - 總計：450 個虛擬節點均勻分布

   優勢：
   - 節點增減只影響相鄰節點
   - 增加 1 個節點：只需遷移 1/N 的數據
   - 虛擬節點保證負載均衡

   計算（3 → 4 節點）：
   - 需要遷移：1/4 = 25%
   - 1000 萬 key × 1KB × 25% = 2.5 GB
   - 相比傳統哈希減少 67% 遷移量✅

   權衡：
   - 查找複雜度：O(log N)（二分搜索）
   - 內存開銷：虛擬節點信息
   - 數據遷移：仍需要遷移部分數據

方案 C：跳躍一致性哈希（Jump Consistent Hash）
   優勢：O(1) 空間，O(log N) 時間
   問題：只支持節點順序增加（不支持任意刪除）
```

**選擇：方案 B（一致性哈希 + 虛擬節點）**

**實現細節：**
```go
type ConsistentHash struct {
    hashFunc    func([]byte) uint32
    replicas    int              // 虛擬節點數
    ring        []uint32         // 哈希環（排序）
    hashMap     map[uint32]string // 哈希 → 節點名
    nodes       map[string]bool   // 節點集合
}

func (ch *ConsistentHash) Add(node string) {
    for i := 0; i < ch.replicas; i++ {
        // 創建虛擬節點
        virtualNode := fmt.Sprintf("%s-%d", node, i)
        hash := ch.hashFunc([]byte(virtualNode))

        ch.ring = append(ch.ring, hash)
        ch.hashMap[hash] = node
    }
    sort.Slice(ch.ring, func(i, j int) bool {
        return ch.ring[i] < ch.ring[j]
    })
    ch.nodes[node] = true
}

func (ch *ConsistentHash) Get(key string) string {
    if len(ch.ring) == 0 {
        return ""
    }

    hash := ch.hashFunc([]byte(key))

    // 二分搜索找到順時針最近的節點
    idx := sort.Search(len(ch.ring), func(i int) bool {
        return ch.ring[i] >= hash
    })

    if idx == len(ch.ring) {
        idx = 0  // 環形，回到開頭
    }

    return ch.hashMap[ch.ring[idx]]
}
```

---

### 決策 3：選擇哪種快取策略？

```
問題：如何協調快取與資料庫的讀寫？

✅ 方案 A：Cache-Aside（旁路快取）⭐
   機制：應用程式控制快取邏輯

   讀取流程：
   1. 查詢快取
   2. 命中：返回
   3. 未命中：查資料庫 → 寫入快取 → 返回

   寫入流程：
   1. 刪除快取
   2. 更新資料庫

   優勢：
   - 簡單易懂：邏輯清晰
   - 最常用：大部分場景適用
   - 快取只存在訪問過的數據

   問題：
   - 快取未命中延遲高（需查 DB）
   - 短暫不一致：刪除快取到 DB 更新完成之間

   一致性問題：
   - T1：線程 A 刪除快取
   - T2：線程 B 查詢快取（未命中）→ 查 DB（舊值）
   - T3：線程 A 更新 DB
   - T4：線程 B 寫入快取（舊值）❌

   解決：延遲雙刪
   1. 刪除快取
   2. 更新 DB
   3. 延遲 N 秒後再刪除快取

✅ 方案 B：Write-Through（寫穿）
   機制：同步更新快取和資料庫

   寫入流程：
   1. 更新快取
   2. 更新資料庫（快取負責）
   3. 兩者都成功才返回

   優勢：
   - 一致性好：快取和 DB 同步
   - 快取始終有效：讀取快

   問題：
   - 寫入延遲高：兩次寫入
   - 寫入放大：即使數據不常讀，也寫快取

   適用：讀多寫少，一致性要求高

✅ 方案 C：Write-Back（寫回）
   機制：只寫快取，異步批量寫 DB

   寫入流程：
   1. 更新快取
   2. 標記為髒（dirty）
   3. 立即返回
   4. 後台定期批量刷新到 DB

   優勢：
   - 寫入極快：只寫內存
   - 批量優化：減少 DB 寫入次數

   問題：
   - 數據可能丟失：快取宕機
   - 一致性最弱：DB 延遲更新

   適用：寫多讀少，可容忍數據丟失

對比：
| 策略 | 讀取延遲 | 寫入延遲 | 一致性 | 複雜度 |
|------|---------|---------|--------|--------|
| Cache-Aside | 中（未命中慢） | 低 | 最終一致 | 簡單 |
| Write-Through | 低 | 高 | 強一致 | 中 |
| Write-Back | 低 | 極低 | 弱一致 | 複雜 |
```

**選擇：Cache-Aside（通用）+ Write-Back（寫多場景）**

**實現細節：**
```go
// Cache-Aside
type CacheAside struct {
    cache Cache
    store DataStore
}

func (ca *CacheAside) Get(ctx context.Context, key string) (interface{}, error) {
    // 1. 查快取
    if value, ok := ca.cache.Get(key); ok {
        return value, nil
    }

    // 2. 查資料庫
    value, err := ca.store.Get(ctx, key)
    if err != nil {
        return nil, err
    }

    // 3. 寫快取
    ca.cache.Set(key, value)
    return value, nil
}

func (ca *CacheAside) Set(ctx context.Context, key string, value interface{}) error {
    // 1. 刪除快取
    ca.cache.Delete(key)

    // 2. 更新資料庫
    return ca.store.Set(ctx, key, value)
}

// Write-Back
type WriteBack struct {
    cache     Cache
    store     DataStore
    dirtyKeys map[string]interface{}  // 髒數據
    mu        sync.Mutex
}

func (wb *WriteBack) Set(ctx context.Context, key string, value interface{}) error {
    wb.mu.Lock()
    defer wb.mu.Unlock()

    // 1. 更新快取
    wb.cache.Set(key, value)

    // 2. 標記為髒
    wb.dirtyKeys[key] = value

    return nil  // 立即返回
}

// 後台刷新
func (wb *WriteBack) flushLoop() {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        wb.flush()
    }
}

func (wb *WriteBack) flush() {
    wb.mu.Lock()
    var keysToDelete []string
    for key, value := range wb.dirtyKeys {
        if err := wb.store.Set(context.Background(), key, value); err == nil {
            keysToDelete = append(keysToDelete, key)
        }
    }
    for _, key := range keysToDelete {
        delete(wb.dirtyKeys, key)
    }
    wb.mu.Unlock()
}
```

---

### 決策 4：如何處理快取問題？

```
快取系統的三大經典問題

問題 1：快取穿透（Cache Penetration）
場景：查詢不存在的數據
問題：每次都打到資料庫

範例：
- 查詢 user_id=-1（不存在）
- 快取無 → 查 DB 無 → 返回 null
- 惡意攻擊：大量查詢不存在的 ID

❌ 方案 A：快取空值
   問題：大量空值占用內存

✅ 方案 B：Bloom Filter
   機制：
   - 啟動時將所有 key 加入 Bloom Filter
   - 查詢時先檢查 Bloom Filter
   - 不存在：直接返回（不查 DB）
   - 可能存在：查快取/DB

   優勢：
   - 空間效率：1 億 key，1% 誤判率 → 1.2 GB
   - 速度快：O(k) 檢查（k 為哈希函數數）

   權衡：
   - 誤判率：1% 的不存在 key 會查 DB
   - 更新複雜：新增數據需更新 BF

---

問題 2：快取雪崩（Cache Avalanche）
場景：大量快取同時過期
問題：瞬間所有請求打到資料庫

範例：
- 批量導入 1 萬筆數據，TTL = 1 小時
- 1 小時後同時過期
- 1 萬個請求同時查 DB ❌

✅ 方案 A：隨機 TTL
   TTL = base + random(0, delta)
   例如：1 小時 ± 10 分鐘

✅ 方案 B：永不過期 + 後台更新
   - 設置 TTL = 0（永不過期）
   - 後台定期刷新數據

---

問題 3：快取擊穿（Cache Breakdown）
場景：熱門數據過期瞬間
問題：大量並發查詢同時打到資料庫

範例：
- 熱門商品快取過期
- 100 個並發請求同時到達
- 100 個請求都查 DB（重複查詢）❌

❌ 方案 A：分布式鎖
   機制：第一個請求加鎖查 DB，其他等待
   問題：增加延遲

✅ 方案 B：永不過期（熱門數據）
   機制：
   - 識別熱門數據（訪問次數閾值）
   - 設置 TTL = 0
   - 後台定期更新

✅ 方案 C：提前更新
   機制：
   - 在過期前 N 秒觸發更新
   - 確保不會真正過期
```

**已實現：** 基本策略
**教學簡化：** Bloom Filter、分布式鎖
**生產環境建議：** 綜合使用多種方案

---

## 📈 擴展性分析

### 當前架構容量

```
單機快取：
- 算法：LRU/LFU
- 容量：16 GB 內存
- 性能：100K RPS
- 延遲：< 1ms

分布式快取：
- 節點：3 個（3 × 16 GB = 48 GB）
- 一致性哈希：150 個虛擬節點/物理節點
- 性能：50K RPS/節點（網絡開銷）

結論：3 節點可支撐 150K RPS，48 GB 容量
```

### 10x 擴展（1.5M RPS，480 GB）

```
方案：增加節點到 30 個
- 容量：30 × 16 GB = 480 GB
- 性能：30 × 50K = 1.5M RPS
- 一致性哈希自動平衡負載

數據遷移：
- 3 → 30 節點
- 每個新節點分擔：1/30 ≈ 3.3% 數據
- 遷移量：48 GB × 27/30 = 43.2 GB
- 遷移時間：~10 分鐘（假設 100 MB/s）

成本：
- 單節點：$100/月（AWS r5.large）
- 30 節點：$3,000/月
```

### 100x 擴展（15M RPS，4.8 TB）

```
需要架構升級：

1. 多層快取
   - L1：本地內存（應用內）
     - 容量：每實例 1 GB
     - 命中率：50%
     - 延遲：< 100ns

   - L2：分布式快取（Redis Cluster）
     - 容量：4.8 TB
     - 命中率：40%（L1 未命中的）
     - 延遲：< 2ms

   - L3：資料庫
     - 命中率：10%（L1+L2 未命中的）
     - 延遲：~20ms

   效果：
   - 總命中率：50% + 40% + 10% = 100%
   - 平均延遲：50% × 0.1ms + 40% × 2ms + 10% × 20ms = 2.85ms

2. 分片策略
   - 300 個快取節點（16 GB each）
   - 按 key hash 分片
   - 每個 shard：50K RPS

3. 副本策略
   - 3 副本（高可用）
   - 讀請求可從任意副本讀取
   - 寫請求需同步到所有副本

   寫入流程：
   - 主副本寫入成功 → 返回
   - 異步同步到從副本
   - 最終一致性（通常 < 1s）

4. 一致性保證
   - 強一致性讀：讀主副本
   - 最終一致性讀：讀任意副本（更快）
   - 配置化：按業務需求選擇

5. 成本估算
   - 快取節點：300 × $100 = $30,000/月
   - 負載均衡：$500/月
   - 監控系統：$1,000/月
   - 總計：~$31,500/月
```

---

## 🔧 實現範圍標註

### ✅ 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **LRU 算法** | `lru.go:18-135` | HashMap + 雙向鏈表，O(1) 操作 |
| **LFU 算法** | `lfu.go:45-214` | 頻率桶，防快取污染 |
| **一致性哈希** | `consistent.go` | 虛擬節點，減少遷移 |
| **Cache-Aside** | `aside.go` | 旁路快取模式 |
| **Write-Back** | `back.go` | 異步批量寫入 |
| **並發安全** | 各算法 `sync.RWMutex` | 讀寫鎖優化 |

### ⚠️ 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **Bloom Filter** | 增加複雜度 | Redis Bloom Filter 插件 |
| **TTL 管理** | 聚焦淘汰算法 | 定期清理過期數據 |
| **副本機制** | 簡化示範 | 3 副本高可用 |
| **序列化** | 假設內存對象 | Protobuf、JSON |
| **持久化** | 純內存快取 | RDB + AOF（Redis 風格）|

### 🚀 生產環境額外需要

```
1. 高可用性
   - 主從複製：1 主 + 2 從
   - 自動故障轉移：Sentinel / Raft
   - 跨機房部署：多機房副本
   - 健康檢查：定期心跳檢測

2. 持久化
   - RDB 快照：定期全量備份
   - AOF 日誌：追加式操作日誌
   - 混合持久化：RDB + AOF 增量

3. 監控告警
   - 命中率：目標 > 80%
   - 延遲分位數：P50/P95/P99
   - 內存使用：告警閾值 > 80%
   - 淘汰率：過高表示容量不足

4. 安全性
   - 認證：密碼 / Token
   - 加密：TLS 傳輸加密
   - 隔離：租戶數據隔離
   - 配額：單租戶容量限制

5. 運維工具
   - 熱 key 分析：識別訪問最頻繁的 key
   - 大 key 檢測：超過閾值的 value
   - 慢查詢日誌：延遲超過 N ms 的操作
   - 數據遷移工具：節點擴縮容
```

---

## 💡 關鍵設計原則總結

### 1. LRU vs LFU（淘汰策略）
```
LRU（時間維度）：
- 優勢：簡單高效，適合通用場景
- 問題：快取污染（一次性訪問）

LFU（頻率維度）：
- 優勢：防污染，保護熱點
- 問題：冷啟動，實現複雜

選擇：
- 訪問模式穩定 → LFU
- 訪問模式多變 → LRU
- 折衷方案：LRU-K, 2Q
```

### 2. 一致性哈希（分布式）
```
傳統哈希 vs 一致性哈希：
- 傳統：hash(key) % N
  - 節點變化 → 大量遷移（~75%）

- 一致性哈希：哈希環 + 虛擬節點
  - 節點變化 → 少量遷移（~1/N）
  - 虛擬節點 → 負載均衡

虛擬節點數量：
- 太少：負載不均
- 太多：內存開銷
- 推薦：150-200 個/物理節點
```

### 3. 快取策略（讀寫模式）
```
Cache-Aside（最常用）：
- 應用程式控制邏輯
- 適合讀多寫少場景

Write-Through（強一致）：
- 同步寫快取和 DB
- 寫入延遲高

Write-Back（高性能）：
- 異步批量寫 DB
- 寫入快，但可能丟數據

選擇：
- 一般場景 → Cache-Aside
- 強一致性 → Write-Through
- 寫多場景 → Write-Back
```

### 4. 快取問題（三大經典）
```
快取穿透：查詢不存在的數據
- 解決：Bloom Filter 預檢

快取雪崩：大量同時過期
- 解決：隨機 TTL

快取擊穿：熱點過期
- 解決：永不過期 + 後台更新

預防為主，監控為輔
```

---

## 📚 延伸閱讀

### 相關系統設計問題
- 如何設計一個 **Redis**？（完整快取系統）
- 如何設計一個 **Memcached**？（分布式快取）
- 如何設計一個 **CDN**？（邊緣快取）

### 快取算法詳解
- **LRU**：LinkedIn, MySQL Buffer Pool
- **LFU**：Caffeine（Java）
- **ARC**：Adaptive Replacement Cache
- **LIRS**：Low Inter-reference Recency Set

### 工業實現參考
- **Redis**：單線程，RDB+AOF 持久化
- **Memcached**：多線程，LRU 淘汰
- **Caffeine**：Java 高性能快取，W-TinyLFU
- **Guava Cache**：Java 快取庫

---

## 🎯 總結

Distributed Cache 展示了**高性能快取系統**的經典設計模式：

1. **LRU/LFU**：O(1) 淘汰算法，HashMap + 雙向鏈表
2. **一致性哈希**：虛擬節點減少遷移，支持動態擴縮容
3. **Cache-Aside**：應用控制快取邏輯，簡單實用
4. **Write-Back**：異步批量寫入，高性能寫入場景

**核心思想：** 用淘汰算法控制內存使用，用一致性哈希實現水平擴展，用快取策略協調讀寫，用多層快取優化性能。

**適用場景：** 資料庫查詢快取、API 響應快取、會話存儲、熱點數據加速

**不適用：** 強一致性要求（金融交易）、數據量極小（不值得快取）

**與其他服務對比：**
| 維度 | Distributed Cache | URL Shortener | Counter Service |
|------|-------------------|---------------|-----------------|
| **核心挑戰** | 淘汰算法 | 唯一 ID | 高頻寫入 |
| **讀寫比** | 100:1 | 100:1 | 10:1 |
| **一致性** | 最終一致 | 強一致 | 最終一致 |
| **延遲要求** | < 1ms | < 10ms | < 50ms |
| **擴展方式** | 一致性哈希 | Snowflake | 批量寫入 |
