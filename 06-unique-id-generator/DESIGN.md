# Unique ID Generator 系統設計文檔

## 問題定義

### 業務需求
構建分布式唯一 ID 生成器，用於：
- **訂單號生成**：每秒數萬筆訂單需要唯一 ID
- **用戶 ID**：全球用戶註冊需要唯一標識
- **分布式追蹤**：微服務鏈路追蹤的 Trace ID
- **資料庫主鍵**：分庫分表場景的全局唯一主鍵

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **唯一性** | 100%（絕對不重複） | 如何在分布式環境保證？ |
| **生成速度** | 10K ID/s（單機） | 如何達到高吞吐？ |
| **延遲** | < 1ms | 如何避免網絡調用？ |
| **有序性** | 趨勢遞增 | 為何需要有序？ |
| **可擴展** | 支持 1024+ 節點 | 如何分配機器 ID？ |

### 容量估算
```
假設：
- 日活用戶：1 億
- 每用戶產生 ID：10 個/天
- 峰值係數：3x（高峰時段）

計算：
- 日均 ID 數：1 億 × 10 = 10 億
- 平均 QPS：10 億 / 86400 ≈ 11,600
- 峰值 QPS：11,600 × 3 = 34,800
- 單機容量：10,000 QPS → 需要 4 個節點

ID 空間：
- 64-bit 整數：2^64 ≈ 1.8 × 10^19
- 按當前速度：可用 500+ 萬年
```

---

## 設計決策樹

### 決策 1：選擇哪種 ID 生成策略？

```
需求：生成全局唯一、趨勢遞增的 64-bit 整數 ID

方案 A：資料庫自增 ID（AUTO_INCREMENT）
   機制：MySQL AUTO_INCREMENT，單表遞增

   優勢：
   - 實現簡單：CREATE TABLE ... id BIGINT AUTO_INCREMENT
   - 嚴格遞增：1, 2, 3, 4...
   - 事務安全：資料庫保證

   問題：
   - 單點瓶頸：所有 ID 生成都要查資料庫
   - 性能極限：單機 ~5,000 QPS
   - 擴展困難：分庫後 ID 會衝突
   - 網絡依賴：每次生成都要網絡調用

   範例（分庫衝突）：
   - DB1: id=1, 2, 3...
   - DB2: id=1, 2, 3...  ← 衝突！

   解決方案（號段模式）：
   - DB1: 1-1000
   - DB2: 1001-2000
   - 每次預取一個號段，減少資料庫訪問
   - 但仍需中心化協調

方案 B：UUID（通用唯一識別碼）
   機制：UUID v4（隨機）或 UUID v1（時間戳 + MAC 地址）

   UUID v4 範例：
   550e8400-e29b-41d4-a716-446655440000
   128-bit，16 進制表示

   優勢：
   - 全局唯一：碰撞概率 < 10^-15
   - 無需協調：本地生成
   - 無狀態：不依賴任何服務

   問題：
   - 過長：128-bit = 16 bytes（vs 64-bit = 8 bytes）
   - 無序：隨機生成，不利於資料庫索引
   - 不可讀：用戶無法記憶

   性能影響（資料庫）：
   - MySQL InnoDB 使用 B+Tree 索引
   - 順序 ID：新插入總在最右邊（高效）
   - UUID：隨機插入，導致頁分裂（碎片化）
   - 寫入性能：順序 ID 比 UUID 快 2-3x

方案 C：ULID（Universally Unique Lexicographically Sortable Identifier）
   機制：48-bit 時間戳 + 80-bit 隨機數

   ULID 範例：
   01ARZ3NDEKTSV4RRFFQ69G5FAV
   26 字符，Base32 編碼

   優勢：
   - 時間有序：前 48-bit 為毫秒時間戳
   - 可排序：字典序 = 時間序
   - URL 友好：Base32（無特殊字符）

   問題：
   - 仍然過長：26 字符（vs 雪花 7-11 字符 Base62）
   - 精度較低：毫秒級（vs 雪花的更細粒度）
   - 隨機部分大：80-bit（vs 雪花 12-bit 序列號）

方案 D：Snowflake（雪花算法）   機制：64-bit 整數 = 時間戳 + 機器 ID + 序列號

   結構：
   ```
   0 - 00000000 00000000 00000000 00000000 00000000 0 - 00000 00000 - 000000000000
   ↑   ↑                                              ↑               ↑
   符  時間戳（41-bit）                                機器ID（10-bit）  序列號（12-bit）
   號
   位
   ```

   分解：
   - 1 bit：符號位（始終為 0，保證正數）
   - 41 bit：時間戳（毫秒）
     - 起始時間（epoch）：2024-01-01
     - 可用時間：2^41 ms ≈ 69 年
   - 10 bit：機器 ID
     - 支持 2^10 = 1024 台機器
   - 12 bit：序列號
     - 同一毫秒內可生成 2^12 = 4096 個 ID

   優勢：
   - 64-bit 整數：緊湊高效
   - 趨勢遞增：時間戳遞增 → ID 遞增
   - 高性能：本地生成，無網絡調用
   - 高吞吐：4096 ID/ms = 400 萬 ID/s

   權衡：
   - 時鐘依賴：依賴系統時間，時鐘回撥會有問題
   - 機器 ID 管理：需要分配唯一的機器 ID
   - 不嚴格遞增：只是趨勢遞增（同一毫秒內無序）

   範例：
   ```
   ID: 123456789012345
   時間戳: (123456789012345 >> 22) + epoch → 2024-03-15 10:30:45.123
   機器 ID: (123456789012345 >> 12) & 0x3FF → 5
   序列號: 123456789012345 & 0xFFF → 789
   ```
```

**選擇：方案 D（Snowflake）**

**為何選擇 Snowflake？**
1. **性能**：本地生成，無網絡開銷
2. **有序性**：時間戳遞增，有利於資料庫索引
3. **緊湊性**：64-bit 整數，節省存儲
4. **可讀性**：可解析出時間、機器、序列號（調試友好）

---

### 決策 2：如何處理時鐘回撥？

```
問題：系統時間被 NTP 校正，時鐘往回調整

時鐘回撥場景：
T0: 系統時間 2024-03-15 10:30:00.123
T1: NTP 校正，時間回撥到 10:29:58.000
T2: 生成 ID 時，時間戳 < 上次生成時的時間戳
    → 可能產生重複 ID ❌

範例：
- T0: 生成 ID，timestamp=100000
- T1: 時鐘回撥 2 秒，timestamp=98000
- T2: 再次生成 ID，timestamp=98000
  - 如果序列號也重置 → ID 重複！

方案 A：拒絕生成（等待時鐘追上）
   機制：檢測到回撥時，拒絕生成 ID

   ```go
   if timestamp < lastTimestamp {
       return 0, errors.New("clock moved backwards")
   }
   ```

   問題：
   - 服務不可用：回撥期間無法生成 ID
   - 影響範圍：如果回撥 1 小時，則服務停 1 小時

方案 B：自旋等待
   機制：等待系統時鐘追上 lastTimestamp

   ```go
   for timestamp <= lastTimestamp {
       time.Sleep(1 * time.Millisecond)
       timestamp = getCurrentMillis()
   }
   ```

   問題：
   - 延遲增加：回撥 5 秒，等待 5 秒
   - 資源浪費：CPU 空轉
   - 仍有短暫不可用

方案 C：序列號擴展 + 拒絕（混合策略）
   機制：
   1. 檢測到回撥 → 記錄回撥量
   2. 使用上次時間戳 + 擴展序列號空間
   3. 如果序列號耗盡 → 拒絕生成

   ```go
   if timestamp < lastTimestamp {
       // 回撥檢測
       offset := lastTimestamp - timestamp
       if offset > maxOffset {
           return 0, errors.New("clock moved backwards too much")
       }

       // 繼續使用 lastTimestamp，但記錄回撥
       timestamp = lastTimestamp
       clockBackCount++
   }
   ```

   優勢：
   - 容忍小回撥：< 1 秒的回撥可處理
   - 不影響服務：繼續生成 ID
   - 有上限：大回撥仍拒絕（防止無限擴展）

   限制：
   - 同一毫秒內序列號會更快耗盡
   - 大回撥（> 1 秒）仍會失敗

方案 D：備用時鐘源
   機制：
   - 使用單調時鐘（monotonic clock）
   - 或使用專用時間服務器（如 Google TrueTime）

   ```go
   // Go 1.9+ time.Now() 已包含 monotonic clock
   start := time.Now()
   elapsed := time.Since(start)  // 單調遞增，不受 NTP 影響
   ```

   但 Snowflake 需要絕對時間戳（可持久化、跨重啟），
   單調時鐘重啟後會重置。
```

**選擇：方案 C（容忍小回撥 + 記錄監控）**

**實現細節：**
```go
const maxBackwardOffset = 5000 // 最多容忍 5 秒回撥

func (g *Generator) Generate() (int64, error) {
    timestamp := currentMillis()

    if timestamp < g.lastTimestamp {
        offset := g.lastTimestamp - timestamp

        // 小回撥：使用上次時間戳，記錄告警
        if offset <= maxBackwardOffset {
            log.Warn("clock moved backwards", offset)
            timestamp = g.lastTimestamp
            // 序列號繼續遞增（可能更快耗盡）
        } else {
            // 大回撥：拒絕生成
            return 0, ErrClockMovedBackwards
        }
    }

    // 正常流程：生成 ID
    // ...
}
```

---

### 決策 3：如何分配機器 ID？

```
問題：1024 個機器需要唯一的 ID（0-1023）

方案 A：手動配置
   機制：每台機器配置文件指定 machineID

   配置範例：
   ```yaml
   # server-001.yaml
   machine_id: 1

   # server-002.yaml
   machine_id: 2
   ```

   問題：
   - 人工錯誤：可能重複配置
   - 擴容困難：新機器需要手動分配
   - 縮容浪費：下線機器 ID 無法回收

方案 B：IP 地址哈希
   機制：hash(IP) % 1024

   ```go
   ip := getLocalIP()  // 如 192.168.1.100
   machineID := hash(ip) % 1024
   ```

   問題：
   - 衝突風險：不同 IP 可能哈希到相同 ID
   - DHCP 變化：IP 變化導致 ID 變化
   - Docker 環境：容器 IP 經常變化

方案 C：ZooKeeper / etcd 分配
   機制：中心化協調服務分配 ID

   流程：
   1. 服務啟動 → 連接 ZooKeeper
   2. 創建臨時節點：/id-generator/nodes/[seq]
   3. ZooKeeper 自動分配序號（0, 1, 2...）
   4. 服務獲取序號作為 machineID
   5. 服務下線 → 臨時節點刪除 → ID 可回收

   ```go
   // ZooKeeper 分配
   conn := connectZK()
   path := "/id-generator/nodes/"
   node, err := conn.CreateEphemeralSequential(path, data)
   // node: /id-generator/nodes/0000000001
   machineID := extractSequence(node)  // 1
   ```

   優勢：
   - 自動分配：無需人工干預
   - 防衝突：ZooKeeper 保證唯一性
   - 自動回收：節點下線 ID 可重用
   - 動態擴縮：新節點自動獲取 ID

   權衡：
   - 依賴外部服務：ZooKeeper/etcd 必須高可用
   - 啟動依賴：ZooKeeper 不可用則無法啟動
   - 複雜度增加：需要維護 ZooKeeper 集群

方案 D：數據中心 ID + 機器 ID（兩級結構）
   機制：10-bit 拆分為 5-bit 數據中心 + 5-bit 機器

   ```
   10-bit 機器 ID：
   [5-bit 數據中心][5-bit 機器]
   ↓
   支持 32 個數據中心 × 32 台機器 = 1024 個節點
   ```

   配置：
   ```yaml
   datacenter_id: 1  # 北京機房
   worker_id: 5      # 機器 5
   machine_id: (1 << 5) | 5 = 37
   ```

   優勢：
   - 邏輯清晰：數據中心隔離
   - 配置簡單：只需配兩個小數字
   - 無依賴：不需要 ZooKeeper

   問題：
   - 仍需手動配置：數據中心 ID 和機器 ID
   - 容量限制：每個數據中心只能 32 台（可能不夠）
```

**選擇：方案 C（ZooKeeper）用於生產，方案 D（兩級）用於教學**

**實現細節（ZooKeeper）：**
```go
type MachineIDAllocator struct {
    zk *zk.Conn
}

func (a *MachineIDAllocator) Allocate() (int64, error) {
    // 創建臨時順序節點
    path := "/id-generator/nodes/"
    node, err := a.zk.CreateProtectedEphemeralSequential(path, []byte{}, zk.WorldACL(zk.PermAll))
    if err != nil {
        return 0, err
    }

    // 解析序號
    // /id-generator/nodes/0000000042 → 42
    machineID := parseSequence(node)

    if machineID > 1023 {
        return 0, errors.New("too many nodes, max 1024")
    }

    return machineID, nil
}
```

---

### 決策 4：如何提升可用性？

```
問題：單個 ID 生成器故障如何處理？

方案 A：單實例 + 快速重啟
   問題：
   - 重啟期間服務不可用（即使 1 秒也不可接受）
   - 無法應對機房故障

方案 B：多實例部署 + 負載均衡
   機制：
   - 部署 N 個 ID 生成器（不同機器 ID）
   - 負載均衡器隨機分發請求

   架構：
   ```
   Client → Load Balancer
             ↓
             ├─ Generator 1 (machineID=1)
             ├─ Generator 2 (machineID=2)
             ├─ Generator 3 (machineID=3)
             └─ ...
   ```

   優勢：
   - 高可用：單實例故障不影響服務
   - 高吞吐：多實例並行生成
   - 水平擴展：按需增加實例

   問題：
   - 每個請求都有網絡開銷（~1-2ms）
   - 對比本地生成（~0.01ms）

方案 C：客戶端嵌入 SDK
   機制：
   - 每個服務內嵌 ID 生成器
   - 啟動時從 ZooKeeper 獲取機器 ID
   - 本地生成 ID，無需網絡調用

   ```go
   // 服務啟動時初始化
   machineID := allocator.GetMachineID()
   generator := snowflake.NewGenerator(machineID)

   // 業務代碼中直接生成
   orderID := generator.Generate()
   ```

   優勢：
   - 零延遲：本地生成
   - 高吞吐：每個服務獨立生成
   - 無網絡依賴：生成階段無網絡調用

   權衡：
   - SDK 維護：需要多語言 SDK
   - 機器 ID 消耗：每個服務實例佔一個 ID
```

**選擇：方案 C（客戶端 SDK）用於高性能場景，方案 B（服務端）用於簡化部署**

---

## 擴展性分析

### 當前架構容量

```
單實例 Snowflake：
- 吞吐量：400 萬 ID/s（理論）
- 實際 QPS：10,000（受 CPU、並發限制）
- 延遲：< 0.1ms

10 個實例（負載均衡）：
- 總 QPS：100,000
- 容量：足夠支撐 10 億用戶規模
```

### 10x 擴展（100 萬 QPS）

```
方案：客戶端 SDK 嵌入
- 每個微服務內嵌 ID 生成器
- 100 個微服務 × 10 實例 = 1000 個生成器
- 每個生成器：1,000 QPS
- 總容量：100 萬 QPS

機器 ID 分配：
- 1024 個 ID 可用
- 100 個微服務 × 10 實例 = 1000 個
- 剩餘：24 個（足夠）

成本：
- ZooKeeper：3 節點 × $100 = $300/月
- 無額外成本（嵌入到業務服務）
```

### 100x 擴展（1000 萬 QPS）

```
瓶頸分析：
機器 ID 不足：只有 1024 個

方案：擴展機器 ID 位數
- 當前：10-bit 機器 ID → 1024 個
- 擴展：12-bit 機器 ID → 4096 個
  - 犧牲 2-bit 序列號
  - 序列號：12-bit → 10-bit（1024 個/毫秒）
  - 仍足夠：100 萬 QPS/毫秒

調整後結構：
```
0 - [41-bit 時間戳] - [12-bit 機器ID] - [10-bit 序列號]
                      4096 個機器      1024 ID/ms
```

容量：
- 4096 個實例 × 2,500 QPS = 1000 萬 QPS ✅

替代方案：多 epoch（多套系統）
- 系統 A：epoch=2024-01-01，1024 個機器
- 系統 B：epoch=2024-06-01，1024 個機器
- 總計：2048 個機器
- ID 不衝突（不同 epoch，時間戳不同）
```

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Snowflake 算法** | `generator.go` | 64-bit 結構、位運算 |
| **時鐘回撥處理** | `generator.go` | 容忍小回撥策略 |
| **UUID/ULID 對比** | `DESIGN.md` | 不同方案權衡 |
| **性能基準測試** | `generator_test.go` | 吞吐量測試 |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **ZooKeeper 分配** | 簡化依賴 | etcd/ZooKeeper 自動分配機器 ID |
| **監控指標** | 聚焦算法 | 時鐘回撥次數、生成失敗率 |
| **HTTP API** | 避免網絡開銷 | 提供 gRPC/HTTP 接口 |
| **多語言 SDK** | Go 示範 | Java/Python/Node.js SDK |

### 生產環境額外需要

```
1. 機器 ID 管理
   - ZooKeeper/etcd 自動分配
   - 節點下線自動回收
   - 防止 ID 衝突檢查
   - 機器 ID 持久化（重啟復用）

2. 時鐘同步
   - NTP 服務器配置
   - 時鐘偏移監控（> 1ms 告警）
   - 多時鐘源驗證
   - 時鐘跳躍記錄

3. 高可用
   - 多數據中心部署
   - 客戶端 SDK（零網絡延遲）
   - 降級方案（UUID 備用）
   - 健康檢查接口

4. 監控告警
   - 生成成功率：> 99.99%
   - 時鐘回撥次數：每小時告警
   - 序列號溢出次數
   - 生成延遲 P99

5. ID 驗證
   - 唯一性檢查（定期掃描）
   - 時間戳合理性驗證
   - 機器 ID 合法性驗證
   - ID 解析工具（調試用）
```

---

## 關鍵設計原則總結

### 1. Snowflake 結構（緊湊 + 有序 + 可解析）
```
64-bit = 時間戳（41） + 機器ID（10） + 序列號（12）

優勢：
- 緊湊：64-bit 整數
- 有序：時間戳遞增
- 高效：本地生成
- 可解析：能還原時間、機器、序列號

容量：
- 69 年時間範圍
- 1024 台機器
- 400 萬 ID/秒
```

### 2. 時鐘回撥處理（容忍 + 監控）
```
策略：
- 小回撥（< 5s）：容忍，使用上次時間戳
- 大回撥（> 5s）：拒絕生成
- 記錄監控：回撥次數、偏移量

為何不能忽略？
- ID 重複風險
- 破壞有序性
- 影響業務邏輯（如按 ID 排序）
```

### 3. 機器 ID 分配（中心化 vs 配置）
```
ZooKeeper 方案（推薦）：
- 自動分配，防衝突
- 節點下線 ID 可回收
- 動態擴縮容

配置文件方案（簡單）：
- 無外部依賴
- 適合小規模部署
- 需要人工管理
```

### 4. 客戶端 SDK（低延遲）
```
為何嵌入客戶端？
- 零網絡延遲（< 0.1ms vs 1-2ms）
- 高吞吐（無網絡瓶頸）
- 無單點故障

權衡：
- 機器 ID 消耗多
- SDK 維護成本
- 多語言支持
```

---

## 延伸閱讀

### 相關系統設計問題
- 如何設計一個 **分布式鎖**？（類似的中心化協調）
- 如何設計一個 **分庫分表方案**？（需要全局唯一 ID）
- 如何設計一個 **訂單系統**？（需要唯一訂單號）

### ID 生成算法詳解
- **Snowflake**：Twitter 開源，最廣泛使用
- **Sonyflake**：Sony 變種，更長時間範圍
- **ULID**：時間有序的 UUID 替代品
- **ObjectId**：MongoDB 的 12-byte ID

### 工業實現參考
- **Twitter Snowflake**：原始實現（已歸檔）
- **Instagram ID**：修改版 Snowflake（41-bit 時間戳 + 13-bit shard + 10-bit 序列）
- **MongoDB ObjectId**：4-byte 時間戳 + 5-byte 隨機 + 3-byte 計數器
- **UUID v7**：新標準，時間有序

---

## 總結

Unique ID Generator 展示了**分布式系統**的核心挑戰：

1. **唯一性保證**：通過時間戳 + 機器 ID + 序列號組合
2. **有序性優化**：時間戳遞增，利於資料庫索引
3. **時鐘問題**：容忍小回撥，拒絕大回撥
4. **中心化協調**：ZooKeeper 分配機器 ID，防衝突

**核心思想：** 用時間戳提供全局順序，用機器 ID 提供空間分區，用序列號提供同一時空的多個 ID。

**適用場景：** 訂單號、用戶 ID、Trace ID、分庫分表主鍵、任何需要全局唯一標識的場景

**不適用：** 需要嚴格遞增（用資料庫自增）、無時間依賴需求（用 UUID）、小規模單機（用自增）

**與其他服務對比：**
| 維度 | Unique ID Gen | URL Shortener | Counter Service |
|------|---------------|---------------|-----------------|
| **核心挑戰** | 全局唯一 | 全局唯一 + 短碼 | 高頻計數 |
| **生成速度** | 10K QPS | 500 QPS | 10K QPS |
| **存儲** | 無（無狀態） | PostgreSQL | Redis + PG |
| **有序性** | 趨勢遞增 | 趨勢遞增 | 無 |
| **時鐘依賴** | 強 | 強 | 無 |
