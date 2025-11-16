# 分布式鍵值存儲系統設計

## 前言

這是一個關於分布式鍵值存儲系統的完整設計文檔，採用蘇格拉底式教學法，通過真實災難場景展示系統的演進過程。

**核心主題**：
- CAP 定理的實戰權衡
- Amazon Dynamo 架構
- 一致性哈希與虛擬節點
- 向量時鐘（Vector Clock）
- Gossip 協議
- Quorum 讀寫

**真實案例**：
- Amazon Dynamo（2007）
- Apache Cassandra
- Riak KV

---

## 第一幕：Double 11 的災難

### 場景：產品會議室，凌晨 2:30

「購物狂歡」電商平台技術總監李明臉色鐵青，會議室的氣氛凝重得令人窒息。

**李明**（技術總監）：「給我解釋一下，為什麼今天 Double 11 凌晨 0 點，我們的購物車服務宕機了整整 18 分鐘？」

**小張**（後端工程師，聲音顫抖）：「Redis 服務器突然宕機了...我們用的是單機 Redis 存儲所有購物車數據...」

**李明**：「損失多少？」

**小陳**（運營經理）：「初步估算...NT$ 4,200 萬。18 分鐘內，120 萬用戶的購物車全部清空，超過 8 萬筆訂單流失...」

會議室一片死寂。

**李明**（深吸一口氣）：「說吧，為什麼選擇單機 Redis？」

**小張**：「因為...簡單。我們把用戶的購物車數據存在 Redis 裡：」

```go
// 購物車數據結構
type ShoppingCart struct {
    UserID    string
    Items     []CartItem
    UpdatedAt time.Time
}

type CartItem struct {
    ProductID string
    Quantity  int
    Price     float64
}

// 單機 Redis 操作
func (s *CartService) AddToCart(userID, productID string, quantity int) error {
    key := fmt.Sprintf("cart:%s", userID)

    // 從 Redis 獲取購物車
    data, err := s.redis.Get(ctx, key).Result()
    if err != nil && err != redis.Nil {
        return err
    }

    var cart ShoppingCart
    if data != "" {
        json.Unmarshal([]byte(data), &cart)
    } else {
        cart.UserID = userID
        cart.Items = make([]CartItem, 0)
    }

    // 添加商品
    found := false
    for i, item := range cart.Items {
        if item.ProductID == productID {
            cart.Items[i].Quantity += quantity
            found = true
            break
        }
    }

    if !found {
        cart.Items = append(cart.Items, CartItem{
            ProductID: productID,
            Quantity:  quantity,
        })
    }

    cart.UpdatedAt = time.Now()

    // 寫回 Redis
    cartData, _ := json.Marshal(cart)
    return s.redis.Set(ctx, key, cartData, 24*time.Hour).Err()
}
```

**小張**：「這個方案很快，讀寫延遲只有 1-2ms...但是今天凌晨 0 點，流量突然暴增到每秒 50 萬次請求，Redis 服務器 CPU 100%，然後...掛了。」

**李明**：「掛了之後呢？」

**小張**（低頭）：「所有購物車數據都在內存裡...全丟了。重啟 Redis 後是空的。」

### 災難分析

**單點故障（Single Point of Failure, SPOF）**：

```
客戶端
   ↓
Redis (單機)  ← 宕機 = 所有數據丟失
   ↓
數據丟失
```

**問題**：
1. **可用性 = 0**：Redis 宕機 = 服務完全不可用
2. **數據丟失**：內存數據未持久化
3. **無法擴展**：單機性能有上限（50萬 QPS 就撐不住）

**李明**：「明天早上 9 點之前，給我一個解決方案。」

---

## 第二幕：主從複製的誕生

### 場景：第二天早上 8:30

小張熬了一夜，眼睛佈滿血絲。

**小張**：「我找到了！Redis 主從複製（Master-Slave Replication）！」

**架構圖**：

```
          寫入
客戶端 --------→ Master Redis
                    ↓
                 數據同步
                    ↓
          ┌────────┴────────┐
          ↓                 ↓
      Slave 1           Slave 2
          ↑                 ↑
          └────── 讀取 ──────┘
              客戶端
```

**實現**：

```go
type CartService struct {
    master *redis.Client    // 寫入
    slaves []*redis.Client  // 讀取
    slaveIndex int
}

func NewCartService(masterAddr string, slaveAddrs []string) *CartService {
    // Master 連接
    master := redis.NewClient(&redis.Options{
        Addr: masterAddr,
    })

    // Slave 連接
    slaves := make([]*redis.Client, len(slaveAddrs))
    for i, addr := range slaveAddrs {
        slaves[i] = redis.NewClient(&redis.Options{
            Addr: addr,
        })
    }

    return &CartService{
        master: master,
        slaves: slaves,
    }
}

// 寫入：只寫 Master
func (s *CartService) AddToCart(userID, productID string, quantity int) error {
    key := fmt.Sprintf("cart:%s", userID)

    // ... 購物車邏輯 ...

    // 寫入 Master
    return s.master.Set(ctx, key, cartData, 24*time.Hour).Err()
}

// 讀取：從 Slave 輪詢
func (s *CartService) GetCart(userID string) (*ShoppingCart, error) {
    key := fmt.Sprintf("cart:%s", userID)

    // 輪詢選擇一個 Slave
    slave := s.getSlave()

    data, err := slave.Get(ctx, key).Result()
    if err != nil {
        return nil, err
    }

    var cart ShoppingCart
    json.Unmarshal([]byte(data), &cart)
    return &cart, nil
}

// 輪詢選擇 Slave
func (s *CartService) getSlave() *redis.Client {
    s.slaveIndex = (s.slaveIndex + 1) % len(s.slaves)
    return s.slaves[s.slaveIndex]
}
```

**小張**（興奮）：「這樣就解決了單點問題！Master 掛了，我們可以手動提升 Slave 為 Master！」

**性能測試**：

```
單機 Redis:
- 讀寫混合：50萬 QPS → 宕機

主從複製:
- 寫入：Master 20萬 QPS
- 讀取：2 個 Slave × 40萬 QPS = 80萬 QPS
- 總計：100萬 QPS ✓
```

**李明**：「很好，上線吧。」

### 三個月後：黑色星期五災難

凌晨 2:00，告警簡訊如雪片般飛來。

**告警**：
```
購物車數據異常：
- 用戶 A 投訴：購物車裡的 iPhone 變成了 2 台（明明只加了 1 台）
- 用戶 B 投訴：剛加入的商品消失了
- 用戶 C 投訴：已刪除的商品又出現了
```

**小張**（查看日誌）：「我發現問題了...是主從延遲！」

### 主從延遲問題

**時間線**：

```
T1 (00:00:00.000):
客戶端 A → Master: 添加 iPhone × 1

T2 (00:00:00.001):
Master → Slave: 開始同步...

T3 (00:00:00.050):  ← 延遲 50ms
客戶端 A → Slave: 讀取購物車 → 空的！（同步還沒完成）

T4 (00:00:00.051):
客戶端 A 以為沒加成功，再次添加 → Master: 添加 iPhone × 1

T5 (00:00:00.100):
最終結果：iPhone × 2 ❌
```

**數據流圖**：

```
時間 T1:  Master [iPhone×1]   Slave []        ← 同步延遲
時間 T2:  客戶端從 Slave 讀取 → 空的
時間 T3:  客戶端再次寫入 Master
時間 T4:  Master [iPhone×2]   Slave [iPhone×1]  ← 數據不一致
```

**監控數據**：

```
主從同步延遲統計（黑色星期五）:
- P50: 20ms
- P95: 100ms
- P99: 500ms
- P99.9: 2000ms  ← 2 秒！

影響：
- 數據不一致報告：12,450 起
- 重複下單：3,200 筆
- 損失：NT$ 680 萬
```

**李明**（憤怒）：「為什麼會有延遲？！」

**小張**：「主從複製是異步的...Master 寫入成功後立即返回，不等 Slave 同步完成。高並發時，Slave 同步跟不上。」

**異步複製流程**：

```go
// Master 寫入流程
func (m *Master) Set(key, value string) error {
    // 1. 寫入 Master 本地
    m.data[key] = value

    // 2. 立即返回（不等 Slave）
    // 3. 異步同步到 Slave（後台線程）
    go m.replicateToSlaves(key, value)

    return nil  // ← 立即返回
}

// 異步同步到 Slaves
func (m *Master) replicateToSlaves(key, value string) {
    for _, slave := range m.slaves {
        // 可能會延遲、失敗
        slave.Set(key, value)
    }
}
```

**李明**：「那就改成同步複製！等所有 Slave 都同步完成再返回！」

---

## 第三幕：強一致性的陷阱

小張實現了同步複製。

```go
// 同步複製
func (m *Master) Set(key, value string) error {
    // 1. 寫入 Master
    m.data[key] = value

    // 2. 同步複製到所有 Slave（等待完成）
    var wg sync.WaitGroup
    errCh := make(chan error, len(m.slaves))

    for _, slave := range m.slaves {
        wg.Add(1)
        go func(s *Slave) {
            defer wg.Done()
            if err := s.Set(key, value); err != nil {
                errCh <- err
            }
        }(slave)
    }

    wg.Wait()
    close(errCh)

    // 3. 檢查是否所有 Slave 都成功
    for err := range errCh {
        if err != nil {
            return err  // 任何一個 Slave 失敗都返回錯誤
        }
    }

    return nil  // 所有 Slave 都成功
}
```

**小張**：「這樣就能保證強一致性了！讀取時，Slave 的數據一定是最新的！」

**性能測試**：

```
寫入延遲：
- 異步複製：2ms
- 同步複製：15ms（等待 Slave 確認）

吞吐量：
- 異步複製：20萬 QPS
- 同步複製：8萬 QPS  ← 下降 60%
```

**李明**：「延遲增加可以接受，一致性更重要。上線！」

### 兩週後：Double 12 災難

**時間**：Double 12 當天，0:15 AM

**告警風暴**：
```
ERROR: 購物車服務不可用
ERROR: Redis Master 無法連接 Slave-2
ERROR: 寫入超時（>5s）
```

**小張**（驚慌）：「Slave-2 所在的機房網絡故障了！Master 無法連接 Slave-2，所有寫入都失敗了！」

**網絡分區圖**：

```
        機房 A（北京）           |  機房 B（上海）
                                |
    ┌────────┐                 |    ┌────────┐
    │ Master │ ────────────────┼────│ Slave-1│
    └────────┘                 |    └────────┘
         │                      |
         × ← 網絡中斷            |    ┌────────┐
         │                      |    │ Slave-2│ ← 無法連接
         ↓                      |    └────────┘
    寫入失敗！                  |
```

**災難數據**：

```
服務不可用時間：42 分鐘
影響用戶：230 萬
流失訂單：15.6 萬筆
直接損失：NT$ 8,700 萬
```

**李明**（咆哮）：「為什麼一個 Slave 宕機，整個服務就不可用了？！」

**小張**：「因為我們要求所有 Slave 都確認...這是強一致性的代價...」

### CAP 定理的覺醒

技術總監請來了資深架構師老王。

**老王**：「你們遇到的，正是 CAP 定理的經典困境。」

**CAP 定理**：

```
C (Consistency)    - 一致性：所有節點看到相同的數據
A (Availability)   - 可用性：系統總是能響應請求
P (Partition tolerance) - 分區容錯：網絡分區時系統仍能工作

定理：分布式系統只能同時滿足其中兩個！
```

**三種選擇**：

```
1. CP（一致性 + 分區容錯）：
   - 網絡分區時，犧牲可用性
   - 例子：銀行轉賬、HBase、MongoDB（強一致模式）

2. AP（可用性 + 分區容錯）：
   - 網絡分區時，犧牲一致性（最終一致性）
   - 例子：購物車、DNS、Cassandra、Dynamo

3. CA（一致性 + 可用性）：
   - 無法容忍網絡分區
   - 例子：單機數據庫、RDBMS
```

**老王**：「你們的購物車服務，選擇了 CP。但是購物車真的需要強一致性嗎？」

**李明**：「什麼意思？」

**老王**：「想想看：如果用戶加了一個商品到購物車，延遲 100ms 才能看到，會怎樣？」

**小張**：「用戶可能會...再點一次？」

**老王**：「對，這是不一致的後果：輕微的用戶體驗問題。但是如果服務不可用呢？」

**李明**（恍然大悟）：「用戶什麼都做不了，直接流失...損失更大！」

**老王**：「購物車不是銀行轉賬，它不需要強一致性。我們應該選擇 AP！」

### AP vs CP 對比

**銀行轉賬（必須 CP）**：

```
場景：A 轉賬 $100 給 B

如果選擇 AP：
T1: A 餘額 -$100 (寫入節點 1)
T2: B 餘額 +$100 (寫入節點 2，但網絡分區無法同步)
結果：節點 1 認為 A 少了 $100，節點 2 認為 B 多了 $100
      → 憑空多了 $100！❌

必須選擇 CP：
- 網絡分區時，拒絕寫入
- 保證 A 和 B 的餘額總是一致的
```

**購物車（選擇 AP）**：

```
場景：用戶添加商品到購物車

如果選擇 CP：
- 網絡分區時，服務不可用
- 用戶無法購物 → 流失

選擇 AP：
- 網絡分區時，仍然可以購物
- 可能會有短暫的不一致（例如同步延遲）
- 但最終會一致
```

**老王**：「Amazon Dynamo 就是一個經典的 AP 系統，專門為購物車設計的。我們來實現它。」

---

## 第四幕：分片的誕生

**老王**：「首先，我們要解決擴展性問題。單個 Master 撐不住更高的寫入量，需要分片（Sharding）。」

### 哈希分片

**基本思路**：

```
將數據按 key 的哈希值分配到不同的節點

哈希函數：
node_index = hash(key) % N

N = 節點數量
```

**架構圖**：

```
                    客戶端
                      ↓
              計算 hash(key) % 3
                ↙     ↓     ↘
           Node-0  Node-1  Node-2
           cart:A  cart:B  cart:C
           cart:D  cart:E  cart:F
```

**實現**：

```go
type DistributedKVStore struct {
    nodes []*Node
}

type Node struct {
    ID     int
    Addr   string
    client *redis.Client
}

func (kv *DistributedKVStore) Get(key string) (string, error) {
    // 計算節點索引
    nodeIndex := kv.getNodeIndex(key)
    node := kv.nodes[nodeIndex]

    // 從對應節點讀取
    return node.client.Get(ctx, key).Result()
}

func (kv *DistributedKVStore) Set(key, value string) error {
    // 計算節點索引
    nodeIndex := kv.getNodeIndex(key)
    node := kv.nodes[nodeIndex]

    // 寫入對應節點
    return node.client.Set(ctx, key, value, 0).Err()
}

func (kv *DistributedKVStore) getNodeIndex(key string) int {
    // 哈希函數
    h := fnv.New32a()
    h.Write([]byte(key))
    hashValue := h.Sum32()

    // 取模
    return int(hashValue % uint32(len(kv.nodes)))
}
```

**數據分布示例**：

```go
// 3 個節點
nodes := []*Node{
    {ID: 0, Addr: "node-0:6379"},
    {ID: 1, Addr: "node-1:6379"},
    {ID: 2, Addr: "node-2:6379"},
}

// 數據分布
hash("cart:user-A") % 3 = 0 → Node-0
hash("cart:user-B") % 3 = 1 → Node-1
hash("cart:user-C") % 3 = 2 → Node-2
hash("cart:user-D") % 3 = 0 → Node-0
hash("cart:user-E") % 3 = 1 → Node-1
```

**小張**：「這樣每個節點只處理 1/3 的數據和流量，寫入能力提升 3 倍！」

**性能測試**：

```
3 個節點：
- 每節點寫入：8萬 QPS
- 總寫入：24萬 QPS ✓

6 個節點：
- 總寫入：48萬 QPS ✓

橫向擴展成功！
```

### 三個月後：擴容災難

業務快速增長，需要從 3 個節點擴容到 4 個節點。

**小張**：「我加了一個節點，結果...所有用戶的購物車都消失了！」

**老王**：「這是哈希分片的經典問題：節點數量改變時，數據需要大規模遷移。」

### 數據遷移問題

**擴容前**（3 個節點）：

```
hash("cart:user-A") % 3 = 0 → Node-0
hash("cart:user-B") % 3 = 1 → Node-1
hash("cart:user-C") % 3 = 2 → Node-2
```

**擴容後**（4 個節點）：

```
hash("cart:user-A") % 4 = 3 → Node-3 ← 改變了！
hash("cart:user-B") % 4 = 2 → Node-2 ← 改變了！
hash("cart:user-C") % 4 = 1 → Node-1 ← 改變了！
```

**數據遷移比例**：

```
擴容前：3 節點
擴容後：4 節點

需要遷移的 key 數量 = ?

計算：
- 總共 N 個 key
- 擴容後，hash(key) % 4 的結果改變的概率 = 75%
- 需要遷移：75% × N

結論：75% 的數據需要遷移！
```

**遷移成本**：

```
數據量：120 億條購物車記錄
需要遷移：90 億條（75%）
每條大小：1 KB
總數據：90 GB

網絡傳輸時間：
- 帶寬：1 Gbps = 125 MB/s
- 時間：90,000 MB / 125 MB/s = 720 秒 = 12 分鐘

期間：
- 大量數據找不到（在遷移中）
- 網絡擁堵
- 服務降級
```

**老王**：「這就是為什麼我們需要一致性哈希（Consistent Hashing）。」

---

## 第五幕：一致性哈希的魔法

### 一致性哈希原理

**核心思想**：

```
將哈希空間組織成一個環（0 ~ 2^32-1）
節點和 key 都映射到環上
key 順時針查找最近的節點
```

**哈希環**：

```
                    0
                    ↑
          Node-C ←  |  → Node-A
               ↖    |    ↗
                 ↖  |  ↗
         180° ←――――┼――――→ hash(key)
                 ↙  |  ↘
               ↙    |    ↘
          Node-B    |    key 順時針查找
                    |
                  2^32-1
```

**實現**：

```go
type ConsistentHash struct {
    ring       map[uint32]string  // 哈希值 → 節點地址
    sortedKeys []uint32           // 已排序的哈希值
    nodes      map[string]bool    // 節點集合
    mu         sync.RWMutex
}

func NewConsistentHash() *ConsistentHash {
    return &ConsistentHash{
        ring:  make(map[uint32]string),
        nodes: make(map[string]bool),
    }
}

// 添加節點
func (ch *ConsistentHash) AddNode(nodeAddr string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()

    // 計算節點的哈希值
    hashValue := ch.hash(nodeAddr)

    // 添加到環上
    ch.ring[hashValue] = nodeAddr
    ch.nodes[nodeAddr] = true

    // 更新已排序的鍵
    ch.sortedKeys = append(ch.sortedKeys, hashValue)
    sort.Slice(ch.sortedKeys, func(i, j int) bool {
        return ch.sortedKeys[i] < ch.sortedKeys[j]
    })
}

// 獲取 key 對應的節點
func (ch *ConsistentHash) GetNode(key string) string {
    ch.mu.RLock()
    defer ch.mu.RUnlock()

    if len(ch.sortedKeys) == 0 {
        return ""
    }

    // 計算 key 的哈希值
    hashValue := ch.hash(key)

    // 二分查找：找到第一個 >= hashValue 的節點
    idx := sort.Search(len(ch.sortedKeys), func(i int) bool {
        return ch.sortedKeys[i] >= hashValue
    })

    // 如果找不到，說明 key 在最後，順時針回到第一個節點
    if idx == len(ch.sortedKeys) {
        idx = 0
    }

    return ch.ring[ch.sortedKeys[idx]]
}

// 哈希函數
func (ch *ConsistentHash) hash(key string) uint32 {
    h := fnv.New32a()
    h.Write([]byte(key))
    return h.Sum32()
}
```

**使用示例**：

```go
ch := NewConsistentHash()

// 添加節點
ch.AddNode("node-A:6379")  // hash = 1000
ch.AddNode("node-B:6379")  // hash = 5000
ch.AddNode("node-C:6379")  // hash = 9000

// 查找 key 對應的節點
ch.GetNode("cart:user-1")  // hash = 3000 → node-B (最近的是 5000)
ch.GetNode("cart:user-2")  // hash = 7000 → node-C (最近的是 9000)
ch.GetNode("cart:user-3")  // hash = 500  → node-A (最近的是 1000)
```

### 一致性哈希的優勢

**擴容時的數據遷移**：

```
擴容前（3 個節點）：
                    0
                    ↑
          Node-C ←  |  → Node-A (hash=1000)
          (9000) ↖  |    ↗
                 ↖  |  ↗
         180° ←――――┼――――→
                 ↙  |  ↘
               ↙    |    ↘
          Node-B    |
          (5000)    |
                  2^32-1

擴容後（4 個節點）：
                    0
                    ↑
          Node-C ←  |  → Node-A (1000)
          (9000) ↖  |    ↗ ↖ Node-D (3000) 新增
                 ↖  | ↗   ↗
         180° ←――――┼――――→
                 ↙  |  ↘
               ↙    |    ↘
          Node-B    |
          (5000)    |
                  2^32-1
```

**數據遷移分析**：

```
Node-D (hash=3000) 加入後：

受影響的範圍：1000 ~ 3000
- 原本歸屬 Node-B 的 key（hash 在 1000~3000）
- 現在歸屬 Node-D
- 只需要從 Node-B 遷移到 Node-D

其他範圍不受影響：
- 3000 ~ 5000：仍然歸 Node-B
- 5000 ~ 9000：仍然歸 Node-C
- 9000 ~ 1000：仍然歸 Node-A

遷移比例 = (3000-1000) / 2^32 ≈ 0.05% ✓
```

**對比**：

```
普通哈希：
- 3 節點 → 4 節點：遷移 75%
- 4 節點 → 5 節點：遷移 80%

一致性哈希：
- N 節點 → N+1 節點：遷移 1/(N+1)
- 3 節點 → 4 節點：遷移 25%
- 4 節點 → 5 節點：遷移 20%
```

**小張**：「太神奇了！一致性哈希將遷移比例從 75% 降到 25%！」

**老王**：「但還有一個問題...」

### 一週後：負載不均問題

**小張**（查看監控）：「奇怪...Node-A 的 CPU 使用率 80%，但 Node-B 只有 20%？」

**負載分布**：

```
Node-A (hash=1000):   負責範圍 9000 ~ 1000  = 35% 的哈希空間
Node-B (hash=5000):   負責範圍 1000 ~ 5000  = 23% 的哈希空間
Node-C (hash=9000):   負責範圍 5000 ~ 9000  = 42% 的哈希空間

負載不均衡：
- Node-C: 42% 的數據
- Node-A: 35% 的數據
- Node-B: 23% 的數據  ← 最輕鬆

差距：42% / 23% = 1.8 倍
```

**老王**：「這是因為節點在哈希環上分布不均勻。解決方法是：虛擬節點（Virtual Nodes）。」

---

## 第六幕：虛擬節點的均衡術

### 虛擬節點原理

**核心思想**：

```
每個物理節點創建多個虛擬節點
虛擬節點均勻分布在哈希環上
key 映射到虛擬節點，虛擬節點映射到物理節點
```

**實現**：

```go
type ConsistentHashWithVNodes struct {
    ring           map[uint32]string  // 哈希值 → 物理節點地址
    sortedKeys     []uint32           // 已排序的哈希值
    nodes          map[string]bool    // 物理節點集合
    virtualNodes   int                // 每個物理節點的虛擬節點數量
    mu             sync.RWMutex
}

func NewConsistentHashWithVNodes(virtualNodes int) *ConsistentHashWithVNodes {
    return &ConsistentHashWithVNodes{
        ring:         make(map[uint32]string),
        nodes:        make(map[string]bool),
        virtualNodes: virtualNodes,  // 例如：150
    }
}

// 添加節點（創建多個虛擬節點）
func (ch *ConsistentHashWithVNodes) AddNode(nodeAddr string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()

    // 為每個物理節點創建 N 個虛擬節點
    for i := 0; i < ch.virtualNodes; i++ {
        // 虛擬節點命名：node-A#0, node-A#1, ..., node-A#149
        virtualNodeKey := fmt.Sprintf("%s#%d", nodeAddr, i)

        // 計算虛擬節點的哈希值
        hashValue := ch.hash(virtualNodeKey)

        // 將虛擬節點映射到物理節點
        ch.ring[hashValue] = nodeAddr
        ch.sortedKeys = append(ch.sortedKeys, hashValue)
    }

    ch.nodes[nodeAddr] = true

    // 重新排序
    sort.Slice(ch.sortedKeys, func(i, j int) bool {
        return ch.sortedKeys[i] < ch.sortedKeys[j]
    })
}

// GetNode 邏輯不變
func (ch *ConsistentHashWithVNodes) GetNode(key string) string {
    ch.mu.RLock()
    defer ch.mu.RUnlock()

    if len(ch.sortedKeys) == 0 {
        return ""
    }

    hashValue := ch.hash(key)

    idx := sort.Search(len(ch.sortedKeys), func(i int) bool {
        return ch.sortedKeys[i] >= hashValue
    })

    if idx == len(ch.sortedKeys) {
        idx = 0
    }

    // 返回物理節點地址
    return ch.ring[ch.sortedKeys[idx]]
}

func (ch *ConsistentHashWithVNodes) hash(key string) uint32 {
    h := fnv.New32a()
    h.Write([]byte(key))
    return h.Sum32()
}
```

**虛擬節點分布示例**：

```
每個物理節點創建 150 個虛擬節點

哈希環上共有：3 × 150 = 450 個虛擬節點

                    0
                    ↑
      A#12  C#45 ← | → A#3  B#89
      B#23    ↖    |    ↗  C#7
      C#56      ↖  |  ↗   A#91
         180° ←――――┼――――→
      A#144     ↙  |  ↘   B#12
      B#78    ↙    |    ↘ C#103
      C#31  A#67   | B#4  A#22
                   |
                 2^32-1
```

**負載分布測試**：

```go
// 測試負載分布
func TestLoadDistribution() {
    ch := NewConsistentHashWithVNodes(150)  // 每節點 150 個虛擬節點

    ch.AddNode("node-A")
    ch.AddNode("node-B")
    ch.AddNode("node-C")

    // 模擬 100,000 個 key
    distribution := make(map[string]int)

    for i := 0; i < 100000; i++ {
        key := fmt.Sprintf("cart:user-%d", i)
        node := ch.GetNode(key)
        distribution[node]++
    }

    for node, count := range distribution {
        percentage := float64(count) / 1000.0
        fmt.Printf("%s: %d keys (%.2f%%)\n", node, count, percentage)
    }
}

// 輸出
node-A: 33,421 keys (33.42%)
node-B: 33,187 keys (33.19%)
node-C: 33,392 keys (33.39%)

標準差：0.12%  ← 非常均衡！
```

**對比**：

```
無虛擬節點：
- Node-A: 35%
- Node-B: 23%  ← 最小
- Node-C: 42%  ← 最大
- 差距：1.8 倍

有虛擬節點（150 個/節點）：
- Node-A: 33.42%
- Node-B: 33.19%  ← 最小
- Node-C: 33.39%  ← 最大
- 差距：1.01 倍  ✓
```

**小張**：「完美！負載均衡了！」

**老王**：「現在我們解決了擴展性問題，接下來是一致性問題。」

---

## 第七幕：並發寫衝突

### 場景：用戶同時在手機和電腦操作購物車

**時間線**：

```
用戶在兩個設備同時操作購物車：

T1 (00:00:00.000):
  購物車初始狀態：[iPhone]

T2 (00:00:00.100):
  手機端：添加 iPad     → 寫入 Node-A → [iPhone, iPad]

T3 (00:00:00.120):
  電腦端：刪除 iPhone   → 寫入 Node-B → [iPad]

T4 (00:00:00.200):
  同步衝突：
  - Node-A 認為：[iPhone, iPad]
  - Node-B 認為：[iPad]
  - 哪個是對的？
```

**網絡分區導致的衝突**：

```
     機房 A                   機房 B
   ┌────────┐               ┌────────┐
   │ Node-A │               │ Node-B │
   │        │  ← 網絡分區 →  │        │
   └────────┘               └────────┘
       ↑                        ↑
     手機端                    電腦端
   添加 iPad                 刪除 iPhone

   結果：
   Node-A: [iPhone, iPad]
   Node-B: [iPad]

   衝突！
```

**小張**：「我們可以用時間戳來解決！取最新的寫入！」

```go
type CartItem struct {
    ProductID string
    Quantity  int
    Timestamp time.Time  // 添加時間戳
}

// 衝突解決：取時間戳最新的
func ResolveConflict(cartA, cartB *ShoppingCart) *ShoppingCart {
    if cartA.Timestamp.After(cartB.Timestamp) {
        return cartA  // A 更新
    }
    return cartB  // B 更新
}
```

**老王**：「時間戳有個致命問題：時鐘不同步。」

### 時鐘同步問題

**場景**：

```
Node-A 的時鐘：2024-11-16 00:00:00.100
Node-B 的時鐘：2024-11-16 00:00:00.050  ← 慢了 50ms

T1: Node-A 寫入 [iPhone, iPad]  (時間戳 00:00:00.100)
T2: Node-B 寫入 [iPad]          (時間戳 00:00:00.050)

按時間戳比較：
- Node-A 的時間戳 > Node-B
- 結果：[iPhone, iPad] 勝出

但實際上：
- Node-B 的操作是後發生的（刪除 iPhone）
- 正確結果應該是：[iPad]

錯誤！
```

**時鐘漂移統計**：

```
跨機房時鐘偏差（NTP 同步）：
- 平均：10ms
- P95：50ms
- P99：100ms
- 極端情況：500ms+

結論：不能依賴物理時鐘！
```

**老王**：「我們需要邏輯時鐘：向量時鐘（Vector Clock）。」

---

## 第八幕：向量時鐘的魔法

### 向量時鐘原理

**核心思想**：

```
每個節點維護一個向量：記錄每個節點的操作次數

例如 3 個節點：
Node-A 的向量：[A:1, B:0, C:0]  ← A 操作了 1 次
Node-B 的向量：[A:0, B:1, C:0]  ← B 操作了 1 次
```

**數據結構**：

```go
type VectorClock struct {
    // 節點 ID → 版本號
    Clocks map[string]int
}

func NewVectorClock() *VectorClock {
    return &VectorClock{
        Clocks: make(map[string]int),
    }
}

// 增加本節點的版本號
func (vc *VectorClock) Increment(nodeID string) {
    vc.Clocks[nodeID]++
}

// 合並兩個向量時鐘（取每個節點的最大值）
func (vc *VectorClock) Merge(other *VectorClock) {
    for nodeID, version := range other.Clocks {
        if vc.Clocks[nodeID] < version {
            vc.Clocks[nodeID] = version
        }
    }
}

// 比較兩個向量時鐘
func (vc *VectorClock) Compare(other *VectorClock) string {
    // 檢查 vc 是否 <= other (所有維度)
    vcLessOrEqual := true
    vcGreaterOrEqual := true

    allNodes := make(map[string]bool)
    for node := range vc.Clocks {
        allNodes[node] = true
    }
    for node := range other.Clocks {
        allNodes[node] = true
    }

    for node := range allNodes {
        vcVersion := vc.Clocks[node]
        otherVersion := other.Clocks[node]

        if vcVersion > otherVersion {
            vcLessOrEqual = false
        }
        if vcVersion < otherVersion {
            vcGreaterOrEqual = false
        }
    }

    if vcLessOrEqual && vcGreaterOrEqual {
        return "equal"      // 相等
    } else if vcLessOrEqual {
        return "before"     // vc 發生在 other 之前
    } else if vcGreaterOrEqual {
        return "after"      // vc 發生在 other 之後
    } else {
        return "concurrent" // 並發衝突
    }
}
```

### 向量時鐘運作示例

**場景：用戶購物車的演進**

```
初始狀態：
Cart = []
VectorClock = [A:0, B:0, C:0]

操作 1（Node-A）：添加 iPhone
Cart = [iPhone]
VectorClock = [A:1, B:0, C:0]  ← A 的版本號 +1

操作 2（Node-A）：添加 iPad
Cart = [iPhone, iPad]
VectorClock = [A:2, B:0, C:0]  ← A 的版本號 +1

操作 3（Node-B）：從操作 1 的狀態分支，添加 MacBook
Cart = [iPhone, MacBook]
VectorClock = [A:1, B:1, C:0]  ← 從 [A:1] 開始，B +1

現在有兩個版本：
Version 1: [iPhone, iPad]    VectorClock = [A:2, B:0, C:0]
Version 2: [iPhone, MacBook] VectorClock = [A:1, B:1, C:0]

比較向量時鐘：
- A:2 vs A:1 → Version 1 更新
- B:0 vs B:1 → Version 2 更新
- 結論：並發衝突！兩個版本都需要保留
```

**衝突解決**：

```go
type ShoppingCartVersion struct {
    Items       []CartItem
    VectorClock *VectorClock
}

type ShoppingCartWithVersions struct {
    Versions []*ShoppingCartVersion  // 可能有多個並發版本
}

// 寫入購物車
func (kv *DistributedKVStore) SetCart(userID string, cart *ShoppingCart, nodeID string) error {
    key := fmt.Sprintf("cart:%s", userID)

    // 獲取當前的向量時鐘
    currentVersions := kv.GetCartVersions(userID)

    // 創建新版本
    newVersion := &ShoppingCartVersion{
        Items:       cart.Items,
        VectorClock: NewVectorClock(),
    }

    // 合並所有當前版本的向量時鐘
    for _, v := range currentVersions {
        newVersion.VectorClock.Merge(v.VectorClock)
    }

    // 增加本節點的版本號
    newVersion.VectorClock.Increment(nodeID)

    // 保存
    return kv.SaveCartVersion(userID, newVersion)
}

// 讀取購物車（可能返回多個並發版本）
func (kv *DistributedKVStore) GetCartVersions(userID string) []*ShoppingCartVersion {
    key := fmt.Sprintf("cart:%s", userID)

    // 從多個副本讀取
    allVersions := kv.ReadFromReplicas(key)

    // 去除被覆蓋的舊版本
    return kv.reconcileVersions(allVersions)
}

// 調和版本（去除被覆蓋的舊版本）
func (kv *DistributedKVStore) reconcileVersions(versions []*ShoppingCartVersion) []*ShoppingCartVersion {
    result := make([]*ShoppingCartVersion, 0)

    for _, v1 := range versions {
        obsolete := false

        for _, v2 := range versions {
            if v1 == v2 {
                continue
            }

            // 如果 v1 發生在 v2 之前，v1 是過時的
            if v1.VectorClock.Compare(v2.VectorClock) == "before" {
                obsolete = true
                break
            }
        }

        if !obsolete {
            result = append(result, v1)
        }
    }

    return result
}
```

**客戶端衝突解決**：

```go
// 客戶端讀取購物車
versions := kv.GetCartVersions("user-123")

if len(versions) == 1 {
    // 沒有衝突，直接使用
    cart := versions[0]
} else {
    // 有並發衝突，需要解決
    // 策略 1：自動合並
    mergedCart := MergeCarts(versions)

    // 策略 2：返回給用戶選擇（Amazon 的做法）
    // "您的購物車有兩個版本，請選擇保留哪個"
    showConflictToUser(versions)

    // 策略 3：取商品的並集
    cart := UnionCarts(versions)
}

// 自動合並策略：取所有版本的商品並集
func MergeCarts(versions []*ShoppingCartVersion) *ShoppingCart {
    allItems := make(map[string]*CartItem)

    for _, v := range versions {
        for _, item := range v.Items {
            existing, exists := allItems[item.ProductID]
            if !exists {
                allItems[item.ProductID] = &item
            } else {
                // 取最大數量
                if item.Quantity > existing.Quantity {
                    existing.Quantity = item.Quantity
                }
            }
        }
    }

    result := &ShoppingCart{
        Items: make([]CartItem, 0, len(allItems)),
    }
    for _, item := range allItems {
        result.Items = append(result.Items, *item)
    }

    return result
}
```

### 向量時鐘完整示例

**多次操作的演進**：

```
T1: Node-A 寫入 [iPhone]
    VectorClock = [A:1, B:0, C:0]

T2: Node-A 寫入 [iPhone, iPad]
    VectorClock = [A:2, B:0, C:0]

T3: Node-B 從 T1 狀態分支，寫入 [iPhone, MacBook]
    VectorClock = [A:1, B:1, C:0]

T4: Node-C 從 T2 狀態分支，寫入 [iPhone, iPad, AirPods]
    VectorClock = [A:2, B:0, C:1]

當前有 3 個版本：
V1: [iPhone, iPad]          [A:2, B:0, C:0]
V2: [iPhone, MacBook]       [A:1, B:1, C:0]
V3: [iPhone, iPad, AirPods] [A:2, B:0, C:1]

比較：
- V1 vs V3：V3 覆蓋 V1 ([A:2,B:0,C:1] > [A:2,B:0,C:0])
- V2 vs V3：並發衝突
- 最終保留：V2 和 V3

用戶看到兩個版本，自動合並：
最終 = [iPhone, iPad, AirPods, MacBook]  ✓
```

**小張**：「向量時鐘太神奇了！完全不依賴物理時鐘，卻能準確判斷因果關係！」

**老王**：「但還有一個問題：節點故障怎麼辦？」

---

## 第九幕：Gossip 協議的自我修復

### 場景：節點靜默失敗

**小張**（查看監控）：「Node-B 已經宕機 10 分鐘了，但系統沒有發現！很多寫入都失敗了！」

**老王**：「我們需要一個去中心化的故障檢測機制：Gossip 協議。」

### Gossip 協議原理

**核心思想**：

```
每個節點定期（例如每秒）隨機選擇幾個其他節點
交換彼此知道的所有節點信息
通過多輪 gossip，信息最終傳播到整個集群
```

**類比**：

```
就像八卦傳播：
- Alice 告訴 Bob 和 Carol
- Bob 告訴 David 和 Eve
- Carol 告訴 Frank 和 Grace
- ...
很快整個社區都知道了
```

**數據結構**：

```go
type NodeInfo struct {
    ID         string
    Addr       string
    Status     string  // "alive", "suspected", "dead"
    Heartbeat  int64   // 心跳計數器
    UpdatedAt  time.Time
}

type GossipProtocol struct {
    localNode     *NodeInfo
    knownNodes    map[string]*NodeInfo  // 所有已知節點
    mu            sync.RWMutex
    gossipInterval time.Duration        // gossip 間隔（例如 1 秒）
    fanout        int                   // 每次 gossip 的節點數（例如 3）
}

func NewGossipProtocol(nodeID, addr string) *GossipProtocol {
    return &GossipProtocol{
        localNode: &NodeInfo{
            ID:        nodeID,
            Addr:      addr,
            Status:    "alive",
            Heartbeat: 0,
            UpdatedAt: time.Now(),
        },
        knownNodes:     make(map[string]*NodeInfo),
        gossipInterval: 1 * time.Second,
        fanout:         3,
    }
}

// 啟動 Gossip 協議
func (gp *GossipProtocol) Start() {
    // 定期增加本地心跳
    go gp.incrementHeartbeat()

    // 定期 gossip
    go gp.gossipLoop()

    // 定期檢測故障
    go gp.detectFailures()
}

// 增加本地心跳
func (gp *GossipProtocol) incrementHeartbeat() {
    ticker := time.NewTicker(gp.gossipInterval)
    defer ticker.Stop()

    for range ticker.C {
        gp.mu.Lock()
        gp.localNode.Heartbeat++
        gp.localNode.UpdatedAt = time.Now()
        gp.mu.Unlock()
    }
}

// Gossip 循環
func (gp *GossipProtocol) gossipLoop() {
    ticker := time.NewTicker(gp.gossipInterval)
    defer ticker.Stop()

    for range ticker.C {
        gp.gossip()
    }
}

// 執行一輪 gossip
func (gp *GossipProtocol) gossip() {
    gp.mu.RLock()

    // 隨機選擇 fanout 個節點
    targets := gp.selectRandomNodes(gp.fanout)

    // 準備要發送的數據（所有已知節點的信息）
    gossipData := gp.prepareGossipData()

    gp.mu.RUnlock()

    // 向選中的節點發送 gossip 消息
    for _, target := range targets {
        go gp.sendGossip(target, gossipData)
    }
}

// 選擇隨機節點
func (gp *GossipProtocol) selectRandomNodes(count int) []*NodeInfo {
    gp.mu.RLock()
    defer gp.mu.RUnlock()

    nodes := make([]*NodeInfo, 0, len(gp.knownNodes))
    for _, node := range gp.knownNodes {
        if node.ID != gp.localNode.ID && node.Status == "alive" {
            nodes = append(nodes, node)
        }
    }

    // 隨機打亂
    rand.Shuffle(len(nodes), func(i, j int) {
        nodes[i], nodes[j] = nodes[j], nodes[i]
    })

    // 取前 count 個
    if len(nodes) > count {
        nodes = nodes[:count]
    }

    return nodes
}

// 準備 gossip 數據
func (gp *GossipProtocol) prepareGossipData() map[string]*NodeInfo {
    data := make(map[string]*NodeInfo)

    // 包含本地節點
    data[gp.localNode.ID] = gp.localNode

    // 包含所有已知節點
    for id, node := range gp.knownNodes {
        data[id] = node
    }

    return data
}

// 發送 gossip 消息
func (gp *GossipProtocol) sendGossip(target *NodeInfo, data map[string]*NodeInfo) {
    // 通過 HTTP 或 gRPC 發送
    // ...

    // 接收對方的回應（對方也會發送它知道的節點信息）
    response := gp.sendGossipRequest(target.Addr, data)

    // 合併對方的信息
    gp.mergeGossipData(response)
}

// 合併 gossip 數據
func (gp *GossipProtocol) mergeGossipData(incomingData map[string]*NodeInfo) {
    gp.mu.Lock()
    defer gp.mu.Unlock()

    for nodeID, incomingNode := range incomingData {
        existingNode, exists := gp.knownNodes[nodeID]

        if !exists {
            // 新節點，直接添加
            gp.knownNodes[nodeID] = incomingNode
        } else {
            // 已知節點，比較心跳
            if incomingNode.Heartbeat > existingNode.Heartbeat {
                // 接收到更新的信息
                gp.knownNodes[nodeID] = incomingNode
            }
        }
    }
}

// 檢測故障
func (gp *GossipProtocol) detectFailures() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        gp.mu.Lock()

        now := time.Now()
        suspectThreshold := 10 * time.Second  // 10 秒沒更新 → suspected
        deadThreshold := 30 * time.Second     // 30 秒沒更新 → dead

        for nodeID, node := range gp.knownNodes {
            if node.ID == gp.localNode.ID {
                continue
            }

            timeSinceUpdate := now.Sub(node.UpdatedAt)

            if timeSinceUpdate > deadThreshold {
                if node.Status != "dead" {
                    log.Printf("Node %s marked as DEAD", nodeID)
                    node.Status = "dead"
                }
            } else if timeSinceUpdate > suspectThreshold {
                if node.Status == "alive" {
                    log.Printf("Node %s marked as SUSPECTED", nodeID)
                    node.Status = "suspected"
                }
            }
        }

        gp.mu.Unlock()
    }
}

// 獲取所有存活節點
func (gp *GossipProtocol) GetAliveNodes() []*NodeInfo {
    gp.mu.RLock()
    defer gp.mu.RUnlock()

    alive := make([]*NodeInfo, 0)
    for _, node := range gp.knownNodes {
        if node.Status == "alive" {
            alive = append(alive, node)
        }
    }

    return alive
}
```

### Gossip 協議運作示例

**集群有 5 個節點**：

```
初始狀態：
Node-A: 知道 [A, B, C]
Node-B: 知道 [A, B, D]
Node-C: 知道 [A, C, E]
Node-D: 知道 [B, D]
Node-E: 知道 [C, E]

第 1 輪 Gossip：
- A → B: 發送 [A, B, C]，B 學到 C
- B → D: 發送 [A, B, C, D]，D 學到 A, C
- C → E: 發送 [A, C, E]，E 學到 A
- D → B: 發送 [A, B, C, D]
- E → C: 發送 [A, C, E]

第 1 輪後：
Node-A: [A, B, C]
Node-B: [A, B, C, D]
Node-C: [A, C, E]
Node-D: [A, B, C, D]
Node-E: [A, C, E]

第 2 輪 Gossip：
- A → C: 發送 [A, B, C]，C 學到 B
- B → A: 發送 [A, B, C, D]，A 學到 D
- C → A: 發送 [A, B, C, E]，A 學到 E
- D → B: 發送 [A, B, C, D]
- E → C: 發送 [A, C, E]

第 2 輪後：
Node-A: [A, B, C, D, E]  ← 知道所有節點了！
Node-B: [A, B, C, D]
Node-C: [A, B, C, E]
Node-D: [A, B, C, D]
Node-E: [A, C, E]

第 3 輪後：
所有節點都知道 [A, B, C, D, E]  ✓
```

**故障檢測示例**：

```
T0: Node-B 宕機

T1 (1 秒後):
- Node-A gossip 給 Node-C，發送 Node-B 的信息 (heartbeat=100, updated=T0)
- Node-C 發現 Node-B 的心跳沒有增加

T5 (5 秒後):
- 多輪 gossip 後，所有節點都發現 Node-B 的心跳停在 100
- Node-B.UpdatedAt = T0 (5 秒前)

T10 (10 秒後):
- 所有節點將 Node-B 標記為 "suspected"

T30 (30 秒後):
- 所有節點將 Node-B 標記為 "dead"
- 從一致性哈希環中移除 Node-B
- 自動將 Node-B 的數據遷移到其他節點
```

**小張**：「太厲害了！完全去中心化，不需要 master！」

**老王**：「最後一個問題：如何在一致性和可用性之間靈活權衡？」

---

## 第十幕：Quorum 讀寫的智慧

### 場景：不同場景對一致性的需求不同

**小張**：「購物車數據，我們希望高可用性。但是庫存數據，需要強一致性。能不能靈活調整？」

**老王**：「當然可以，這就是 Quorum 讀寫。」

### Quorum 原理

**核心思想**：

```
N = 副本數量（例如 3）
W = 寫入成功的副本數（Write Quorum）
R = 讀取的副本數（Read Quorum）

一致性保證：W + R > N

例如：
N=3, W=2, R=2
→ W + R = 4 > 3  ✓
→ 讀取的 2 個副本中，至少有 1 個包含最新數據
```

**不同配置的權衡**：

```
1. 強一致性（CP）：
   N=3, W=3, R=1
   - 寫入必須所有副本成功（慢，低可用）
   - 讀取從任意副本（快）

2. 最終一致性（AP）：
   N=3, W=1, R=1
   - 寫入只需 1 個副本成功（快，高可用）
   - 讀取只從 1 個副本（快，但可能不是最新）

3. 均衡配置：
   N=3, W=2, R=2
   - 寫入需要 2 個副本成功
   - 讀取從 2 個副本（至少有 1 個是最新的）

4. 讀優化：
   N=3, W=3, R=1
   - 寫入慢，但讀取快

5. 寫優化：
   N=3, W=1, R=3
   - 寫入快，但讀取慢
```

**實現**：

```go
type QuorumConfig struct {
    N int  // 副本數量
    W int  // 寫 Quorum
    R int  // 讀 Quorum
}

type DistributedKVStore struct {
    consistentHash *ConsistentHashWithVNodes
    quorumConfig   *QuorumConfig
    gossip         *GossipProtocol
}

// Quorum 寫入
func (kv *DistributedKVStore) Set(key, value string) error {
    // 1. 找到負責這個 key 的 N 個副本節點
    replicaNodes := kv.getReplicaNodes(key, kv.quorumConfig.N)

    // 2. 並發寫入所有副本
    successCh := make(chan bool, len(replicaNodes))
    errorCh := make(chan error, len(replicaNodes))

    for _, node := range replicaNodes {
        go func(n *NodeInfo) {
            err := kv.writeToNode(n, key, value)
            if err != nil {
                errorCh <- err
            } else {
                successCh <- true
            }
        }(node)
    }

    // 3. 等待 W 個副本成功
    successCount := 0
    for i := 0; i < len(replicaNodes); i++ {
        select {
        case <-successCh:
            successCount++
            if successCount >= kv.quorumConfig.W {
                return nil  // 達到 W，寫入成功
            }
        case err := <-errorCh:
            // 記錄錯誤，但繼續等待
            log.Printf("Write error: %v", err)
        }
    }

    // 4. 沒有達到 W 個成功
    return fmt.Errorf("quorum not met: only %d/%d writes succeeded",
        successCount, kv.quorumConfig.W)
}

// Quorum 讀取
func (kv *DistributedKVStore) Get(key string) (string, error) {
    // 1. 找到負責這個 key 的 N 個副本節點
    replicaNodes := kv.getReplicaNodes(key, kv.quorumConfig.N)

    // 2. 並發讀取 R 個副本
    readCount := kv.quorumConfig.R
    if readCount > len(replicaNodes) {
        readCount = len(replicaNodes)
    }

    type ReadResult struct {
        Value       string
        VectorClock *VectorClock
        Error       error
    }

    resultCh := make(chan *ReadResult, readCount)

    for i := 0; i < readCount; i++ {
        go func(node *NodeInfo) {
            value, vclock, err := kv.readFromNode(node, key)
            resultCh <- &ReadResult{
                Value:       value,
                VectorClock: vclock,
                Error:       err,
            }
        }(replicaNodes[i])
    }

    // 3. 收集 R 個結果
    results := make([]*ReadResult, 0, readCount)
    for i := 0; i < readCount; i++ {
        result := <-resultCh
        if result.Error == nil {
            results = append(results, result)
        }
    }

    if len(results) == 0 {
        return "", fmt.Errorf("all reads failed")
    }

    // 4. 根據向量時鐘選擇最新的版本
    latest := results[0]
    for _, r := range results[1:] {
        if r.VectorClock.Compare(latest.VectorClock) == "after" {
            latest = r
        }
    }

    // 5. Read Repair：如果發現過時的副本，異步修復
    go kv.readRepair(key, latest, results)

    return latest.Value, nil
}

// 獲取副本節點（使用一致性哈希）
func (kv *DistributedKVStore) getReplicaNodes(key string, count int) []*NodeInfo {
    // 1. 找到 key 在環上的位置
    primaryNode := kv.consistentHash.GetNode(key)

    // 2. 順時針找到後續的 count-1 個節點作為副本
    allNodes := kv.gossip.GetAliveNodes()
    replicas := make([]*NodeInfo, 0, count)

    // 添加主節點
    for _, node := range allNodes {
        if node.Addr == primaryNode {
            replicas = append(replicas, node)
            break
        }
    }

    // 添加後續節點
    started := false
    for len(replicas) < count {
        for _, node := range allNodes {
            if node.Addr == primaryNode {
                started = true
                continue
            }
            if started && len(replicas) < count {
                replicas = append(replicas, node)
            }
        }
    }

    return replicas
}

// Read Repair：異步修復過時的副本
func (kv *DistributedKVStore) readRepair(key string, latest *ReadResult, allResults []*ReadResult) {
    for _, result := range allResults {
        if result.VectorClock.Compare(latest.VectorClock) == "before" {
            // 這個副本是過時的，修復它
            log.Printf("Read repair: updating stale replica for key %s", key)
            // 異步寫入最新值
            // ...
        }
    }
}
```

### Quorum 配置對比

**場景 1：購物車（高可用優先）**

```go
config := &QuorumConfig{
    N: 3,  // 3 個副本
    W: 1,  // 寫入 1 個副本即成功
    R: 1,  // 讀取 1 個副本
}

// W + R = 2 < 3，不保證強一致性
// 但可用性極高：只要有 1 個節點存活就能服務

寫入延遲：~5ms（只等 1 個副本）
讀取延遲：~3ms（只讀 1 個副本）
可用性：99.999%
一致性：最終一致性
```

**場景 2：庫存（一致性優先）**

```go
config := &QuorumConfig{
    N: 3,  // 3 個副本
    W: 3,  // 寫入所有副本成功
    R: 1,  // 讀取 1 個副本
}

// W + R = 4 > 3，保證強一致性
// 但可用性較低：1 個節點故障就無法寫入

寫入延遲：~50ms（等所有副本）
讀取延遲：~3ms（只讀 1 個副本）
可用性：95%（1 個節點故障即不可寫）
一致性：強一致性
```

**場景 3：用戶資料（均衡）**

```go
config := &QuorumConfig{
    N: 3,  // 3 個副本
    W: 2,  // 寫入 2 個副本成功
    R: 2,  // 讀取 2 個副本
}

// W + R = 4 > 3，保證強一致性
// 可用性較好：容忍 1 個節點故障

寫入延遲：~20ms（等 2 個副本）
讀取延遲：~10ms（讀 2 個副本）
可用性：99.9%（容忍 1 個節點故障）
一致性：強一致性
```

### 性能對比

**測試數據（3 節點集群）**：

```
配置：N=3, W=1, R=1（高可用）
- 寫入 QPS：120,000
- 讀取 QPS：150,000
- P99 寫延遲：8ms
- P99 讀延遲：5ms
- 節點故障影響：1 個節點故障，仍 100% 可用

配置：N=3, W=2, R=2（均衡）
- 寫入 QPS：80,000
- 讀取 QPS：90,000
- P99 寫延遲：25ms
- P99 讀延遲：18ms
- 節點故障影響：1 個節點故障，仍 100% 可用

配置：N=3, W=3, R=1（強一致）
- 寫入 QPS：40,000
- 讀取 QPS：150,000
- P99 寫延遲：60ms
- P99 讀延遲：5ms
- 節點故障影響：1 個節點故障，寫入不可用
```

**小張**：「太完美了！可以根據不同的數據類型選擇不同的配置！」

---

## 第十一幕：最終架構

### 完整的分布式 KV Store 架構

**架構圖**：

```
                     客戶端
                       ↓
              ┌────────┴────────┐
              ↓                 ↓
          節點 A             節點 B             節點 C
        ┌────────┐         ┌────────┐         ┌────────┐
        │一致性哈希│         │一致性哈希│         │一致性哈希│
        │虛擬節點  │         │虛擬節點  │         │虛擬節點  │
        ├────────┤         ├────────┤         ├────────┤
        │向量時鐘  │         │向量時鐘  │         │向量時鐘  │
        ├────────┤         ├────────┤         ├────────┤
        │Quorum   │         │Quorum   │         │Quorum   │
        │讀寫      │         │讀寫      │         │讀寫      │
        └────────┘         └────────┘         └────────┘
             ↕                  ↕                  ↕
        ┌────────────────────────────────────────────┐
        │         Gossip 協議（節點發現與故障檢測）     │
        └────────────────────────────────────────────┘
```

### 關鍵組件總結

**1. 一致性哈希 + 虛擬節點**：

```
解決問題：
- 節點擴縮容時的數據遷移
- 負載均衡

實現：
- 哈希環
- 每個物理節點 150 個虛擬節點
- 數據遷移比例：O(1/N)
```

**2. 向量時鐘**：

```
解決問題：
- 並發寫衝突檢測
- 不依賴物理時鐘

實現：
- 每個節點維護版本向量
- 比較向量判斷因果關係
- 並發衝突保留多個版本
```

**3. Gossip 協議**：

```
解決問題：
- 去中心化的節點發現
- 故障檢測

實現：
- 定期隨機 gossip
- 心跳機制
- 10 秒 → suspected，30 秒 → dead
```

**4. Quorum 讀寫**：

```
解決問題：
- 一致性與可用性的靈活權衡

實現：
- 可配置的 N, W, R
- W + R > N 保證一致性
- Read Repair 修復過時副本
```

### 核心流程

**寫入流程**：

```
1. 客戶端發起寫入 Set("cart:user-123", value)

2. 計算 key 的哈希值，找到 N 個副本節點
   - 使用一致性哈希
   - 順時針找到 N 個節點

3. 並發寫入 N 個副本
   - 每個副本增加本地的向量時鐘
   - 異步寫入

4. 等待 W 個副本成功
   - W = 1：高可用
   - W = 2：均衡
   - W = 3：強一致性

5. 返回客戶端成功
```

**讀取流程**：

```
1. 客戶端發起讀取 Get("cart:user-123")

2. 計算 key 的哈希值，找到 N 個副本節點

3. 並發讀取 R 個副本
   - R = 1：快速讀取
   - R = 2：均衡
   - R = 3：最新數據保證

4. 比較向量時鐘，選擇最新版本
   - 如果有並發衝突，返回多個版本

5. Read Repair：異步修復過時的副本

6. 返回客戶端
```

**故障檢測流程**：

```
1. 每個節點定期（1 秒）增加自己的心跳

2. 每個節點隨機選擇 3 個其他節點，發送 gossip
   - 發送自己知道的所有節點信息
   - 接收對方知道的所有節點信息

3. 合併信息
   - 心跳更高的版本覆蓋舊版本

4. 檢測故障
   - 10 秒沒更新 → suspected
   - 30 秒沒更新 → dead

5. 從一致性哈希環中移除死亡節點
```

### 最終性能

**性能測試（10 節點集群）**：

```
配置：N=3, W=2, R=2

寫入性能：
- QPS：800,000
- P50 延遲：10ms
- P99 延遲：35ms
- P99.9 延遲：80ms

讀取性能：
- QPS：1,500,000
- P50 延遲：5ms
- P99 延遲：15ms
- P99.9 延遲：40ms

可用性：
- 1 個節點故障：100% 可用
- 2 個節點故障：100% 可用
- 3 個節點故障：部分數據不可用（少於 W 個副本）

一致性：
- 強一致性（W + R > N）
- 並發寫衝突自動檢測
- 客戶端可選擇衝突解決策略
```

**擴展性測試**：

```
節點數量 vs 總 QPS：

3 節點：   300,000 QPS
6 節點：   600,000 QPS
10 節點：  1,000,000 QPS
20 節點：  2,000,000 QPS
50 節點：  5,000,000 QPS

線性擴展！✓
```

---

## 第十二幕：真實案例

### Amazon Dynamo（2007）

**背景**：

Amazon 購物車服務需要極高的可用性。

**核心設計**：

```
1. 一致性哈希 + 虛擬節點
   - 每個節點 100-200 個虛擬節點

2. 向量時鐘
   - 檢測並發衝突
   - 客戶端解決衝突（例如：取商品並集）

3. Quorum (N=3, W=2, R=2)
   - 可配置

4. Gossip 協議
   - 節點發現
   - 故障檢測

5. Hinted Handoff
   - 節點臨時不可用時，寫入提示副本
   - 節點恢復後，提示副本傳回數據

6. Merkle Tree
   - 高效的數據同步
   - 檢測副本之間的差異
```

**性能**：

```
- 可用性：99.9995%（4 個 9）
- P99.9 寫延遲：< 300ms
- P99.9 讀延遲：< 300ms
```

### Apache Cassandra

**背景**：

Facebook 開發，用於 Inbox 搜索。

**核心設計**：

```
1. 繼承 Dynamo 的分區和副本策略
   - 一致性哈希
   - 虛擬節點（vnodes）

2. 可調節的一致性級別
   - ONE, QUORUM, ALL
   - LOCAL_QUORUM（同機房）
   - EACH_QUORUM（每個機房）

3. Gossip 協議
   - 完全去中心化
   - 無單點故障

4. LSM Tree 存儲引擎
   - 高性能寫入
   - Compaction 策略
```

**應用場景**：

```
- Netflix：視頻推薦
- Apple：iCloud
- Uber：行程數據
- Instagram：用戶動態
```

### Riak KV

**背景**：

商業級分布式 KV Store，基於 Dynamo 論文。

**核心設計**：

```
1. 向量時鐘（Dotted Version Vectors）
   - 改進的向量時鐘算法
   - 更高效的衝突檢測

2. Active Anti-Entropy
   - 主動檢測數據不一致
   - Merkle Tree 同步

3. 多種後端
   - Bitcask（默認）
   - LevelDB
   - Memory

4. CRDTs（Conflict-free Replicated Data Types）
   - 自動解決衝突的數據類型
   - Counter, Set, Map, Register
```

---

## 總結

### 演進歷程回顧

```
1. 單機 Redis
   ↓ 宕機 → 損失 4,200 萬

2. 主從複製
   ↓ 主從延遲 → 數據不一致

3. 同步複製（CP）
   ↓ 網絡分區 → 服務不可用

4. CAP 定理覺醒
   ↓ 選擇 AP（可用性優先）

5. 哈希分片
   ↓ 擴容 → 75% 數據遷移

6. 一致性哈希
   ↓ 負載不均

7. 虛擬節點
   ↓ 並發寫衝突

8. 向量時鐘
   ↓ 節點故障無法檢測

9. Gossip 協議
   ↓ 一致性無法靈活配置

10. Quorum 讀寫
   ↓ 完美！
```

### 關鍵技術總結

| 技術 | 解決問題 | 核心原理 |
|------|----------|----------|
| 一致性哈希 | 節點擴縮容時的數據遷移 | 哈希環 + 順時針查找 |
| 虛擬節點 | 負載不均衡 | 每個物理節點創建多個虛擬節點 |
| 向量時鐘 | 並發寫衝突檢測 | 邏輯時鐘，記錄每個節點的操作次數 |
| Gossip 協議 | 節點發現與故障檢測 | 定期隨機 gossip，心跳機制 |
| Quorum 讀寫 | 一致性與可用性權衡 | W + R > N 保證一致性 |

### CAP 定理實踐

```
購物車服務的選擇：AP（可用性 + 分區容錯）

理由：
1. 購物車不是強一致性場景
2. 可用性極其重要（宕機 = 用戶流失）
3. 最終一致性可接受
4. 並發衝突可通過向量時鐘檢測並解決

配置：
- N = 3
- W = 2
- R = 2
- 可用性：99.99%
- 一致性：最終一致性
```

### 性能指標

```
最終架構（10 節點集群）：

- 寫入 QPS：800,000
- 讀取 QPS：1,500,000
- P99 寫延遲：35ms
- P99 讀延遲：15ms
- 可用性：99.99%
- 擴展性：線性擴展

vs 單機 Redis：
- QPS：500,000 → 800,000（1.6 倍）
- 可用性：95% → 99.99%
- 擴展性：無 → 線性擴展
```

### 參考資料

1. **論文**：
   - [Dynamo: Amazon's Highly Available Key-value Store](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) (2007)
   - [Cassandra - A Decentralized Structured Storage System](https://www.cs.cornell.edu/projects/ladis2009/papers/lakshman-ladis2009.pdf) (2009)

2. **書籍**：
   - Designing Data-Intensive Applications (Martin Kleppmann)
   - Database Internals (Alex Petrov)

3. **開源項目**：
   - [Apache Cassandra](https://cassandra.apache.org/)
   - [Riak KV](https://riak.com/)
   - [DynamoDB](https://aws.amazon.com/dynamodb/)

---

**恭喜你！你已經掌握了分布式鍵值存儲系統的核心設計！**

