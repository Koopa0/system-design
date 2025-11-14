# Distributed Cache

分散式快取系統，展示 LRU/LFU 淘汰演算法、一致性雜湊、多種快取策略。

## 設計目標

實作生產級快取系統，涵蓋從本地快取到分散式快取的完整演進過程。

## 核心功能

- **淘汰演算法**：LRU、LFU
- **分散式擴展**：一致性雜湊 + 虛擬節點
- **快取策略**：Cache-Aside、Write-Through、Write-Back
- **並發安全**：RWMutex 保護

## 使用方式

### LRU 快取

```go
lru := cache.NewLRU(100)  // 容量 100

lru.Set("user:1001", userData)
value, ok := lru.Get("user:1001")
if ok {
    fmt.Printf("快取命中: %v\n", value)
}
```

### LFU 快取

```go
lfu := cache.NewLFU(100)

lfu.Set("key1", "value1")
value, ok := lfu.Get("key1")

// 查看統計
stats := lfu.GetStats()
fmt.Printf("當前大小: %d, 最小頻率: %d\n", stats.Size, stats.MinFreq)
fmt.Printf("頻率分布: %v\n", stats.FreqDist)
```

### 分散式快取

```go
nodes := []string{"node1", "node2", "node3"}
dc := cache.NewDistributedCache(nodes, func() cache.Cache {
    return cache.NewLRU(1000)
})

dc.Set("user:1001", userData)
value, ok := dc.Get("user:1001")

// 動態新增節點
dc.AddNode("node4", func() cache.Cache {
    return cache.NewLRU(1000)
})
```

### Cache-Aside 策略

```go
lru := cache.NewLRU(1000)
store := NewDataStore()  // 實作 DataStore 介面

aside := strategy.NewCacheAside(lru, store)

// 讀取（自動處理快取邏輯）
value, err := aside.Get(ctx, "user:1001")

// 寫入（先刪快取，再更新 DB）
err := aside.Set(ctx, "user:1001", userData)
```

### Write-Back 策略

```go
wb := strategy.NewWriteBack(cache, store, 5*time.Second)

// 寫入（只寫快取，異步批量刷新到 DB）
wb.Set(ctx, "key1", "value1")

// 停止時刷新所有髒數據
wb.Stop()
```

## 執行

```bash
# 執行示範程式
go run cmd/server/main.go
```

輸出示範：
```
=== LRU 快取示範 ===
已寫入 3 筆資料，當前快取：[c b a]
存取 'a' 後，當前快取：[a c b]
寫入 'd' 後，當前快取：[d a c] (淘汰了 'b')

=== LFU 快取示範 ===
當前快取統計：大小=3, 最小頻率=1, 頻率分布=map[1:2 4:1]
寫入 'd' 後，頻率分布=map[1:2 2:1 4:1] (淘汰了頻率最低的 'c')

=== 分散式快取示範 ===
資料分布：
  node1: 2 筆資料
  node2: 1 筆資料
  node3: 2 筆資料
```

## 效能指標

### LRU

| 操作 | 時間複雜度 | 實際效能 |
|------|-----------|---------|
| Get | O(1) | <100ns |
| Set | O(1) | <100ns |
| 記憶體 | O(n) | ~100 bytes/項 |

### LFU

| 操作 | 時間複雜度 | 實際效能 |
|------|-----------|---------|
| Get | O(1) | <200ns |
| Set | O(1) | <200ns |
| 記憶體 | O(n) | ~150 bytes/項 |

### 一致性雜湊

| 操作 | 時間複雜度 | 實際效能 |
|------|-----------|---------|
| Get | O(log N) | <500ns |
| Add Node | O(N log N) | - |
| Remove Node | O(N log N) | - |

## 測試

```bash
# 單元測試
go test -v ./...

# 並發測試
go test -v -race ./internal/cache
```

測試場景：
- LRU 淘汰正確性
- LFU 頻率計算正確性
- 一致性雜湊資料分布
- 並發安全性

## 實作細節

詳細的系統設計分析請參考 [DESIGN.md](./DESIGN.md)，包含：
- LRU vs LFU vs FIFO 淘汰演算法比較
- 傳統雜湊 vs 一致性雜湊（數據遷移量分析）
- Cache-Aside vs Write-Through vs Write-Back 權衡
- 快取三大問題：穿透、雪崩、擊穿
- 從 150K 到 1.5M 到 15M RPS 的擴展分析
