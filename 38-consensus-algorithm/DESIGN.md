# Chapter 38: 共識演算法 (Consensus Algorithm)

> 使用蘇格拉底方法教學：透過四位工程師的對話，深入理解 Raft 和 Paxos 共識演算法

## 角色介紹

- **Emma**: 資深分散式系統架構師，專精於共識演算法
- **David**: 後端工程師，想要了解如何實作高可用系統
- **Sarah**: 前端工程師，對分散式系統理論感興趣
- **Michael**: DevOps 工程師，負責維運叢集系統

---

## Act 1: 為什麼需要共識演算法？

**場景**：團隊會議室，白板上畫著多個伺服器節點的架構圖

**David**: Emma，我們現在有個問題。我們的訂單系統用了 3 台 MySQL 做主從複寫，但是當主節點掛掉時，我們不知道該選哪台從節點當新的主節點。

**Emma**: 這就是典型的「共識問題」。在分散式系統中，多個節點需要對某個值（比如誰是主節點）達成一致意見。

**Sarah**: 這聽起來很簡單啊，為什麼不讓節點們投票決定？

**Emma**: 很好的想法！但考慮這個場景：假設你有 3 個節點 A、B、C，它們之間的網路連線不穩定。

```
初始狀態：
A (主節點)  ←→  B (從節點)
    ↕
C (從節點)

網路分區後：
A (孤立)        B ←→ C
```

如果 A 與 B、C 之間的網路斷開，B 和 C 可能會選舉出新的主節點，但 A 還以為自己是主節點。這時候就有兩個主節點了！

**David**: 這會導致「腦裂」(Split Brain) 問題對吧？兩個主節點都接受寫入請求，資料就不一致了。

**Emma**: 完全正確！這就是為什麼我們需要一個能夠容忍網路分區的共識演算法。它需要保證：

1. **Safety（安全性）**: 所有節點對同一個決策達成一致，不會出現兩個不同的結果
2. **Liveness（活性）**: 系統最終能做出決策，不會永遠卡住
3. **Fault Tolerance（容錯性）**: 即使部分節點故障或網路分區，系統仍能運作

**Michael**: 所以這就像是在一群人中選出領導者，即使有些人失聯或不回應？

**Emma**: 完全正確！這就是「領導者選舉」（Leader Election）問題，它是共識問題的一個經典應用。今天我們要學習兩個著名的共識演算法：Raft 和 Paxos。

**Sarah**: 為什麼有兩個演算法？一個不夠嗎？

**Emma**: Paxos 是 Leslie Lamport 在 1989 年提出的，理論上很優雅但實作起來非常困難。Raft 是 Diego Ongaro 和 John Ousterhout 在 2014 年提出的，設計目標就是「易於理解、易於實作」。

讓我們先從 Raft 開始學習！

---

## Act 2: Raft 演算法 - Leader Election

**場景**：Emma 在白板上畫出 Raft 的狀態機圖

```
┌─────────────┐
│  Follower   │ ←─── 啟動時的初始狀態
└──────┬──────┘
       │ 選舉超時 (election timeout)
       ↓
┌─────────────┐
│  Candidate  │ ─── 發起選舉，請求投票
└──────┬──────┘
       │ 獲得多數票
       ↓
┌─────────────┐
│   Leader    │ ─── 處理所有客戶端請求
└─────────────┘
```

**Emma**: Raft 把節點分成三種角色：Follower（跟隨者）、Candidate（候選人）、Leader（領導者）。

**David**: 這就像是一個班級選班長的過程？

**Emma**: 非常好的比喻！讓我詳細解釋：

### 1. 初始狀態：所有節點都是 Follower

```go
type NodeState int

const (
    Follower NodeState = iota
    Candidate
    Leader
)

type RaftNode struct {
    id            string
    state         NodeState
    currentTerm   int       // 當前任期
    votedFor      string    // 投票給誰
    electionTimer *time.Timer
    log           []LogEntry
    commitIndex   int
    lastApplied   int

    // Leader 專用
    nextIndex     map[string]int  // 下一個要發送給每個節點的日誌索引
    matchIndex    map[string]int  // 已知已複製到每個節點的最高日誌索引
}
```

**Sarah**: `currentTerm` 是什麼？

**Emma**: 這是 Raft 的核心概念之一。Term（任期）就像是「邏輯時鐘」，每次選舉都會開啟一個新的任期。節點透過比較 term 來判斷資訊的新舊。

```
Timeline:
Term 1: ■■■■■■■■ (Node A is Leader)
Term 2: ■■■■■■■■■■■■■■ (Node B is Leader)
Term 3: ■■ (Election, no winner) - Split Vote
Term 4: ■■■■■■■■ (Node C is Leader)
```

### 2. 選舉超時：Follower 變成 Candidate

**Emma**: 每個 Follower 都有一個隨機的選舉超時時間（通常是 150-300ms）。如果在這段時間內沒有收到 Leader 的心跳，就轉變為 Candidate 發起選舉。

```go
func (rn *RaftNode) startElectionTimer() {
    // 隨機超時時間：150-300ms
    timeout := time.Duration(150+rand.Intn(150)) * time.Millisecond

    rn.electionTimer = time.AfterFunc(timeout, func() {
        rn.mu.Lock()
        defer rn.mu.Unlock()

        if rn.state == Leader {
            return // Leader 不發起選舉
        }

        // 發起選舉
        rn.startElection()
    })
}

func (rn *RaftNode) startElection() {
    // 1. 轉變為 Candidate
    rn.state = Candidate

    // 2. Term +1
    rn.currentTerm++

    // 3. 投票給自己
    rn.votedFor = rn.id
    votesReceived := 1

    // 4. 重置選舉計時器
    rn.startElectionTimer()

    log.Printf("Node %s starting election for term %d", rn.id, rn.currentTerm)

    // 5. 發送 RequestVote RPC 給所有其他節點
    for _, peer := range rn.peers {
        go func(peer string) {
            req := &RequestVoteRequest{
                Term:         rn.currentTerm,
                CandidateID:  rn.id,
                LastLogIndex: len(rn.log) - 1,
                LastLogTerm:  rn.getLastLogTerm(),
            }

            resp := rn.sendRequestVote(peer, req)

            rn.mu.Lock()
            defer rn.mu.Unlock()

            if resp.VoteGranted {
                votesReceived++

                // 獲得多數票？
                if votesReceived > len(rn.peers)/2 {
                    rn.becomeLeader()
                }
            }
        }(peer)
    }
}
```

**David**: 為什麼選舉超時要是隨機的？

**Emma**: 優秀的問題！這是為了避免「選舉衝突」。如果所有節點同時超時，它們都會同時發起選舉，都投票給自己，結果沒人能獲得多數票。

### 3. 投票規則

**Emma**: 當節點收到 RequestVote 請求時，它會根據以下規則決定是否投票：

```go
func (rn *RaftNode) handleRequestVote(req *RequestVoteRequest) *RequestVoteResponse {
    rn.mu.Lock()
    defer rn.mu.Unlock()

    resp := &RequestVoteResponse{
        Term:        rn.currentTerm,
        VoteGranted: false,
    }

    // 規則 1: 如果請求的 term 小於自己的 term，拒絕投票
    if req.Term < rn.currentTerm {
        log.Printf("Node %s rejecting vote for %s: stale term (%d < %d)",
            rn.id, req.CandidateID, req.Term, rn.currentTerm)
        return resp
    }

    // 規則 2: 如果請求的 term 更大，更新自己的 term 並轉為 Follower
    if req.Term > rn.currentTerm {
        rn.currentTerm = req.Term
        rn.state = Follower
        rn.votedFor = ""
    }

    // 規則 3: 在同一個 term 內，只能投票給一個候選人
    if rn.votedFor != "" && rn.votedFor != req.CandidateID {
        log.Printf("Node %s rejecting vote for %s: already voted for %s",
            rn.id, req.CandidateID, rn.votedFor)
        return resp
    }

    // 規則 4: 候選人的日誌至少要跟自己一樣新
    // (這是為了保證不會選出日誌過舊的 Leader)
    lastLogIndex := len(rn.log) - 1
    lastLogTerm := rn.getLastLogTerm()

    logIsUpToDate := req.LastLogTerm > lastLogTerm ||
        (req.LastLogTerm == lastLogTerm && req.LastLogIndex >= lastLogIndex)

    if !logIsUpToDate {
        log.Printf("Node %s rejecting vote for %s: log not up-to-date",
            rn.id, req.CandidateID)
        return resp
    }

    // 所有規則都通過，投票給候選人
    rn.votedFor = req.CandidateID
    rn.startElectionTimer() // 重置選舉計時器
    resp.VoteGranted = true

    log.Printf("Node %s voting for %s in term %d",
        rn.id, req.CandidateID, rn.currentTerm)

    return resp
}
```

**Sarah**: 「日誌至少要跟自己一樣新」是什麼意思？

**Emma**: 這是一個重要的安全保證。Raft 的 Leader 會接收所有客戶端的寫入請求，並將它們記錄在日誌中。如果選出一個日誌過舊的 Leader，它就不知道之前已經提交的資料，會造成資料丟失。

讓我舉個例子：

```
節點狀態：
Node A: [log1, log2, log3, log4] (term 5)
Node B: [log1, log2] (term 3)
Node C: [log1, log2, log3] (term 5)

如果 B 變成 Leader，它的 log3 和 log4 就會丟失！
所以 B 不能獲得 A 和 C 的投票。
```

### 4. 成為 Leader

**Emma**: 當候選人獲得多數票（>= N/2 + 1）時，它就成為新的 Leader：

```go
func (rn *RaftNode) becomeLeader() {
    if rn.state == Leader {
        return // 已經是 Leader
    }

    rn.state = Leader
    rn.electionTimer.Stop()

    log.Printf("Node %s became leader for term %d", rn.id, rn.currentTerm)

    // 初始化 Leader 狀態
    lastLogIndex := len(rn.log)
    for _, peer := range rn.peers {
        rn.nextIndex[peer] = lastLogIndex
        rn.matchIndex[peer] = 0
    }

    // 立即發送心跳，宣告自己是 Leader
    rn.sendHeartbeats()

    // 啟動心跳計時器（每 50ms 發送一次）
    go rn.heartbeatLoop()
}

func (rn *RaftNode) heartbeatLoop() {
    ticker := time.NewTicker(50 * time.Millisecond)
    defer ticker.Stop()

    for {
        <-ticker.C

        rn.mu.Lock()
        if rn.state != Leader {
            rn.mu.Unlock()
            return
        }
        rn.mu.Unlock()

        rn.sendHeartbeats()
    }
}

func (rn *RaftNode) sendHeartbeats() {
    for _, peer := range rn.peers {
        go func(peer string) {
            // 發送空的 AppendEntries (心跳)
            req := &AppendEntriesRequest{
                Term:         rn.currentTerm,
                LeaderID:     rn.id,
                PrevLogIndex: rn.nextIndex[peer] - 1,
                PrevLogTerm:  rn.getLogTerm(rn.nextIndex[peer] - 1),
                Entries:      []LogEntry{}, // 空日誌 = 心跳
                LeaderCommit: rn.commitIndex,
            }

            resp := rn.sendAppendEntries(peer, req)

            rn.mu.Lock()
            defer rn.mu.Unlock()

            // 如果收到更高的 term，退位為 Follower
            if resp.Term > rn.currentTerm {
                rn.currentTerm = resp.Term
                rn.state = Follower
                rn.votedFor = ""
                rn.startElectionTimer()
            }
        }(peer)
    }
}
```

**Michael**: 所以 Leader 會不斷發送心跳，Follower 收到心跳就重置選舉計時器，這樣就不會發起新的選舉了？

**Emma**: 完全正確！這就是 Raft 維持穩定狀態的機制。只要 Leader 健康，Follower 就會一直收到心跳，不會超時。

**David**: 那如果 Leader 掛掉呢？

**Emma**: Follower 會因為收不到心跳而超時，然後發起新的選舉。這就是 Raft 的故障恢復機制。

---

## Act 3: Raft 演算法 - Log Replication

**場景**：Emma 在白板上畫出日誌複製的流程

```
Client          Leader          Follower1       Follower2
  |               |                |               |
  |---(write)---->|                |               |
  |               |--AppendEntries-|->             |
  |               |                |               |
  |               |--AppendEntries-|-------------->|
  |               |                |               |
  |               |<----Success----|               |
  |               |                |               |
  |               |<----Success----|---------------|
  |               |                |               |
  |               | (commit when majority responded)
  |               |                |               |
  |<----OK--------|                |               |
```

**Emma**: 現在我們有了穩定的 Leader，下一步是處理客戶端的寫入請求。Raft 使用「日誌複製」來確保所有節點的資料一致。

### 1. 日誌結構

**Emma**: 每個節點都維護一個日誌，日誌是一系列的指令：

```go
type LogEntry struct {
    Term    int         // 日誌條目所屬的任期
    Index   int         // 日誌條目的索引
    Command interface{} // 實際的指令 (例如: "SET x=5")
}

// 節點的日誌範例：
// Index:   1    2    3    4    5    6
// Term:    1    1    1    2    2    3
// Command: x=1  y=2  z=3  x=4  y=5  z=6
```

**Sarah**: 為什麼每個日誌條目都要記錄 Term？

**Emma**: 這是為了檢測不一致。相同 Index 的日誌條目，如果 Term 不同，就表示日誌發生了分歧。

### 2. 日誌複製流程

**Emma**: 當 Leader 收到客戶端的寫入請求時：

```go
func (rn *RaftNode) handleClientRequest(cmd interface{}) error {
    rn.mu.Lock()

    if rn.state != Leader {
        rn.mu.Unlock()
        return errors.New("not the leader")
    }

    // 1. 將指令追加到自己的日誌
    entry := LogEntry{
        Term:    rn.currentTerm,
        Index:   len(rn.log),
        Command: cmd,
    }
    rn.log = append(rn.log, entry)

    log.Printf("Leader %s appended log entry at index %d", rn.id, entry.Index)

    rn.mu.Unlock()

    // 2. 並行地發送 AppendEntries 給所有 Follower
    successCount := 1 // Leader 自己算一個
    var mu sync.Mutex
    var wg sync.WaitGroup

    for _, peer := range rn.peers {
        wg.Add(1)
        go func(peer string) {
            defer wg.Done()

            if rn.replicateLogToPeer(peer) {
                mu.Lock()
                successCount++
                mu.Unlock()
            }
        }(peer)
    }

    wg.Wait()

    // 3. 如果多數節點回應成功，提交日誌
    if successCount > len(rn.peers)/2 {
        rn.mu.Lock()
        rn.commitIndex = entry.Index
        rn.mu.Unlock()

        // 4. 應用到狀態機
        rn.applyLog(entry)

        return nil
    }

    return errors.New("failed to replicate to majority")
}

func (rn *RaftNode) replicateLogToPeer(peer string) bool {
    rn.mu.Lock()

    nextIdx := rn.nextIndex[peer]
    prevLogIndex := nextIdx - 1
    prevLogTerm := rn.getLogTerm(prevLogIndex)

    // 要發送的日誌條目
    entries := rn.log[nextIdx:]

    req := &AppendEntriesRequest{
        Term:         rn.currentTerm,
        LeaderID:     rn.id,
        PrevLogIndex: prevLogIndex,
        PrevLogTerm:  prevLogTerm,
        Entries:      entries,
        LeaderCommit: rn.commitIndex,
    }

    rn.mu.Unlock()

    resp := rn.sendAppendEntries(peer, req)

    rn.mu.Lock()
    defer rn.mu.Unlock()

    if resp.Success {
        // 更新 nextIndex 和 matchIndex
        rn.nextIndex[peer] = nextIdx + len(entries)
        rn.matchIndex[peer] = rn.nextIndex[peer] - 1
        return true
    } else {
        // 失敗：Follower 的日誌不匹配，退回重試
        if rn.nextIndex[peer] > 0 {
            rn.nextIndex[peer]--
        }
        return false
    }
}
```

**David**: 等等，`PrevLogIndex` 和 `PrevLogTerm` 是用來做什麼的？

**Emma**: 這是 Raft 確保日誌一致性的關鍵！Leader 在發送新日誌之前，會告訴 Follower：「在 index N 的位置，你的日誌 term 應該是 T」。

如果 Follower 發現自己在 index N 的位置沒有日誌，或者 term 不匹配，它會拒絕這次 AppendEntries。

```
Leader:   [1,1] [2,1] [3,2] [4,2] [5,3]
                                    ↑ 要發送 index=5
                          ↑ PrevLogIndex=4, PrevLogTerm=2

Follower: [1,1] [2,1] [3,2] [4,2]
                          ↑ 檢查：index 4 的 term 是否為 2？✓

如果 Follower 的日誌是：
Follower: [1,1] [2,1] [3,2] [4,3]  <- term 不匹配！
                          ↑ 拒絕 AppendEntries
```

### 3. Follower 處理 AppendEntries

```go
func (rn *RaftNode) handleAppendEntries(req *AppendEntriesRequest) *AppendEntriesResponse {
    rn.mu.Lock()
    defer rn.mu.Unlock()

    resp := &AppendEntriesResponse{
        Term:    rn.currentTerm,
        Success: false,
    }

    // 規則 1: 如果 Leader 的 term 小於自己，拒絕
    if req.Term < rn.currentTerm {
        return resp
    }

    // 規則 2: 如果收到更高的 term，更新並轉為 Follower
    if req.Term > rn.currentTerm {
        rn.currentTerm = req.Term
        rn.state = Follower
        rn.votedFor = ""
    }

    // 收到 Leader 的訊息，重置選舉計時器
    rn.startElectionTimer()

    // 規則 3: 日誌一致性檢查
    if req.PrevLogIndex >= 0 {
        // 檢查 PrevLogIndex 是否存在
        if req.PrevLogIndex >= len(rn.log) {
            log.Printf("Follower %s rejecting: log too short (need %d, have %d)",
                rn.id, req.PrevLogIndex, len(rn.log))
            return resp
        }

        // 檢查 PrevLogTerm 是否匹配
        if rn.log[req.PrevLogIndex].Term != req.PrevLogTerm {
            log.Printf("Follower %s rejecting: term mismatch at %d (need %d, have %d)",
                rn.id, req.PrevLogIndex, req.PrevLogTerm, rn.log[req.PrevLogIndex].Term)
            return resp
        }
    }

    // 規則 4: 如果存在衝突的日誌條目，刪除它及其後的所有條目
    for i, entry := range req.Entries {
        index := req.PrevLogIndex + 1 + i

        if index < len(rn.log) {
            // 如果已有日誌且 term 不同，刪除此條目及其後所有條目
            if rn.log[index].Term != entry.Term {
                log.Printf("Follower %s truncating log from index %d", rn.id, index)
                rn.log = rn.log[:index]
            }
        }
    }

    // 規則 5: 追加新的日誌條目
    for i, entry := range req.Entries {
        index := req.PrevLogIndex + 1 + i

        if index >= len(rn.log) {
            rn.log = append(rn.log, entry)
            log.Printf("Follower %s appended log entry at index %d", rn.id, index)
        }
    }

    // 規則 6: 更新 commitIndex
    if req.LeaderCommit > rn.commitIndex {
        rn.commitIndex = min(req.LeaderCommit, len(rn.log)-1)

        // 應用已提交的日誌到狀態機
        for rn.lastApplied < rn.commitIndex {
            rn.lastApplied++
            rn.applyLog(rn.log[rn.lastApplied])
        }
    }

    resp.Success = true
    return resp
}
```

**Sarah**: 我有點困惑。為什麼 Leader 要等多數節點回應才能提交？

**Emma**: 這是 Raft 保證資料持久性的關鍵。如果 Leader 在只有自己寫入日誌後就回應客戶端「成功」，然後 Leader 立刻掛掉，新選出的 Leader 可能沒有這條日誌，資料就丟失了。

但是如果多數節點都寫入了日誌，那麼新選出的 Leader（需要獲得多數票）至少會從一個有這條日誌的節點那裡獲得投票，保證不會選出沒有這條日誌的 Leader。

### 4. 日誌衝突處理

**Michael**: 如果網路分區導致出現兩個 Leader，日誌不一致怎麼辦？

**Emma**: 優秀的問題！讓我舉個例子：

```
初始狀態（Term 1，Node A 是 Leader）：
Node A (Leader): [1:x=1] [1:y=2] [1:z=3]
Node B:          [1:x=1] [1:y=2] [1:z=3]
Node C:          [1:x=1] [1:y=2] [1:z=3]

網路分區發生：
Partition 1: Node A (孤立)
Partition 2: Node B, Node C

Term 2：B 和 C 選舉出新 Leader (Node B)
Node A (舊 Leader): [1:x=1] [1:y=2] [1:z=3] [2:a=4]
Node B (新 Leader): [1:x=1] [1:y=2] [1:z=3] [2:b=5] [2:c=6]
Node C:             [1:x=1] [1:y=2] [1:z=3] [2:b=5] [2:c=6]

網路恢復後：
Node A 收到 Node B 的心跳（Term 2），發現自己的 term 過時，退位為 Follower。
Node B 發送 AppendEntries 給 Node A：
  - PrevLogIndex=3, PrevLogTerm=1 ✓ (匹配)
  - 要追加 [2:b=5] [2:c=6]

Node A 處理：
  - 檢查 index 3：[1:z=3] ✓
  - 追加 [2:b=5]，但發現 index 4 已有 [2:a=4]
  - Term 不同（2 != 2），但內容不同，刪除 [2:a=4]
  - 追加 [2:b=5] [2:c=6]

最終狀態：
Node A: [1:x=1] [1:y=2] [1:z=3] [2:b=5] [2:c=6]
Node B: [1:x=1] [1:y=2] [1:z=3] [2:b=5] [2:c=6]
Node C: [1:x=1] [1:y=2] [1:z=3] [2:b=5] [2:c=6]

[2:a=4] 永遠丟失了，因為它從未被多數節點確認。
```

**David**: 所以 Raft 的規則是：**Leader 的日誌永遠是對的，Follower 必須覆蓋自己與 Leader 不一致的日誌**？

**Emma**: 完全正確！這就是 Raft 的「Leader Completeness Property」：一旦日誌條目被提交（多數節點寫入），它就會出現在之後所有 Leader 的日誌中。

---

## Act 4: Paxos 演算法 - Basic Paxos

**場景**：休息後，Emma 開始介紹 Paxos

**Emma**: 現在我們已經理解了 Raft，讓我們來看看 Paxos。Paxos 的理論更加優雅，但也更難理解。

**Sarah**: 我聽說 Paxos 被稱為「分散式系統的聖經」？

**Emma**: 是的，Paxos 由 Leslie Lamport 提出，它解決的是更一般化的「共識問題」：如何讓多個節點對一個值達成一致。

Paxos 有三種角色：
- **Proposer（提案者）**: 提出一個值
- **Acceptor（接受者）**: 對提案進行投票
- **Learner（學習者）**: 學習已經達成共識的值

### Basic Paxos 的兩階段協議

**Emma**: Paxos 分為兩個階段：

```
Phase 1: Prepare (準備階段)
┌───────────┐        ┌───────────┐
│ Proposer  │        │ Acceptors │
└─────┬─────┘        └─────┬─────┘
      │                    │
      │--Prepare(n)------->│
      │                    │
      │<--Promise(n, v)----|
      │                    │

Phase 2: Accept (接受階段)
      │                    │
      │--Accept(n, v)----->│
      │                    │
      │<--Accepted(n, v)---|
      │                    │
```

讓我詳細解釋每個階段：

### Phase 1: Prepare 階段

```go
type ProposalNumber struct {
    Number int    // 提案編號
    NodeID string // 節點 ID（用於打破平局）
}

func (pn ProposalNumber) GreaterThan(other ProposalNumber) bool {
    if pn.Number != other.Number {
        return pn.Number > other.Number
    }
    return pn.NodeID > other.NodeID
}

type Proposer struct {
    id                string
    proposalNumber    ProposalNumber
    proposedValue     interface{}
    promisesReceived  int
    highestAccepted   ProposalNumber
    acceptedValue     interface{}
}

// Phase 1a: Proposer 發送 Prepare 請求
func (p *Proposer) Prepare(acceptors []*Acceptor) error {
    // 生成一個比之前更大的提案編號
    p.proposalNumber = ProposalNumber{
        Number: p.proposalNumber.Number + 1,
        NodeID: p.id,
    }

    log.Printf("Proposer %s sending Prepare with n=%d", p.id, p.proposalNumber.Number)

    // 發送 Prepare 給所有 Acceptor
    for _, acceptor := range acceptors {
        go func(acc *Acceptor) {
            resp := acc.ReceivePrepare(p.proposalNumber)

            if resp.Promise {
                p.handlePromise(resp)
            }
        }(acceptor)
    }

    return nil
}

type Acceptor struct {
    id                 string
    minProposal        ProposalNumber // 承諾不接受更小的提案
    acceptedProposal   ProposalNumber // 已接受的提案編號
    acceptedValue      interface{}    // 已接受的值
    mu                 sync.Mutex
}

type PrepareResponse struct {
    Promise          bool
    AcceptedProposal ProposalNumber
    AcceptedValue    interface{}
}

// Phase 1b: Acceptor 處理 Prepare 請求
func (a *Acceptor) ReceivePrepare(n ProposalNumber) *PrepareResponse {
    a.mu.Lock()
    defer a.mu.Unlock()

    resp := &PrepareResponse{Promise: false}

    // 如果 n 大於之前承諾的提案編號，則承諾
    if n.GreaterThan(a.minProposal) {
        a.minProposal = n
        resp.Promise = true

        // 如果之前已經接受過提案，返回它
        if a.acceptedProposal.Number > 0 {
            resp.AcceptedProposal = a.acceptedProposal
            resp.AcceptedValue = a.acceptedValue
        }

        log.Printf("Acceptor %s promised proposal n=%d", a.id, n.Number)
    } else {
        log.Printf("Acceptor %s rejected proposal n=%d (already promised n=%d)",
            a.id, n.Number, a.minProposal.Number)
    }

    return resp
}
```

**David**: 等等，`minProposal` 是什麼意思？

**Emma**: 這是 Paxos 的核心機制。當 Acceptor 承諾一個提案編號 n 時，它同時承諾**不再接受任何小於 n 的提案**。這確保了舊的提案不會干擾新的提案。

```
時間線：
t1: Proposer A 發送 Prepare(n=1)
    Acceptor X 承諾 n=1，設定 minProposal=1

t2: Proposer B 發送 Prepare(n=2)
    Acceptor X 承諾 n=2，更新 minProposal=2

t3: Proposer A 發送 Accept(n=1, value="A")
    Acceptor X 拒絕，因為 1 < minProposal(2)
    ↑ 這防止了舊提案的干擾
```

**Sarah**: 如果 Acceptor 已經接受過一個值，為什麼要在 Promise 回應中返回它？

**Emma**: 這是確保「一旦值被選定，就不能改變」的關鍵！讓我舉個例子：

```
場景：有 3 個 Acceptor (A, B, C)，需要多數（2 個）同意

步驟 1：Proposer P1 提案 n=1, value="X"
  A: 接受 (1, "X")
  B: 接受 (1, "X")  <- 多數同意，"X" 被選定
  C: 網路故障，未收到

步驟 2：Proposer P2 不知道 "X" 已被選定，發起新提案
  P2 發送 Prepare(n=2)

  A: Promise (已接受 n=1, value="X")  <- 返回已接受的值
  C: Promise (尚未接受任何值)

  P2 收到 Promise 後，發現 A 已經接受了 "X"
  P2 必須使用 "X" 而不是自己想提案的值！

步驟 3：P2 發送 Accept(n=2, value="X")
  A: 接受 (2, "X")
  C: 接受 (2, "X")

最終："X" 仍然是被選定的值，即使有新的 Proposer
```

### Phase 2: Accept 階段

```go
// Proposer 處理 Promise 回應
func (p *Proposer) handlePromise(resp *PrepareResponse) {
    p.promisesReceived++

    // 記錄收到的最高接受提案
    if resp.AcceptedProposal.GreaterThan(p.highestAccepted) {
        p.highestAccepted = resp.AcceptedProposal
        p.acceptedValue = resp.AcceptedValue
    }

    // 獲得多數 Promise？
    if p.promisesReceived > len(acceptors)/2 {
        // 決定要提案的值
        var valueToPropose interface{}
        if p.acceptedValue != nil {
            // 如果有 Acceptor 已經接受過值，必須使用那個值
            valueToPropose = p.acceptedValue
            log.Printf("Proposer %s using previously accepted value: %v",
                p.id, valueToPropose)
        } else {
            // 否則可以使用自己的值
            valueToPropose = p.proposedValue
            log.Printf("Proposer %s using own value: %v",
                p.id, valueToPropose)
        }

        // 進入 Phase 2
        p.sendAccept(valueToPropose)
    }
}

// Phase 2a: Proposer 發送 Accept 請求
func (p *Proposer) sendAccept(value interface{}) {
    log.Printf("Proposer %s sending Accept(n=%d, value=%v)",
        p.id, p.proposalNumber.Number, value)

    acceptedCount := 0

    for _, acceptor := range acceptors {
        go func(acc *Acceptor) {
            resp := acc.ReceiveAccept(p.proposalNumber, value)

            if resp.Accepted {
                acceptedCount++

                // 多數接受，值被選定！
                if acceptedCount > len(acceptors)/2 {
                    log.Printf("Value %v is CHOSEN!", value)
                    p.notifyLearners(value)
                }
            }
        }(acceptor)
    }
}

type AcceptResponse struct {
    Accepted bool
}

// Phase 2b: Acceptor 處理 Accept 請求
func (a *Acceptor) ReceiveAccept(n ProposalNumber, value interface{}) *AcceptResponse {
    a.mu.Lock()
    defer a.mu.Unlock()

    resp := &AcceptResponse{Accepted: false}

    // 只接受 >= minProposal 的提案
    if n.GreaterThan(a.minProposal) || n == a.minProposal {
        a.acceptedProposal = n
        a.acceptedValue = value
        resp.Accepted = true

        log.Printf("Acceptor %s accepted (n=%d, value=%v)",
            a.id, n.Number, value)
    } else {
        log.Printf("Acceptor %s rejected Accept(n=%d) because promised n=%d",
            a.id, n.Number, a.minProposal.Number)
    }

    return resp
}
```

**Michael**: 所以 Paxos 的關鍵是：
1. Prepare 階段確保沒有舊的提案干擾
2. 如果已有值被接受，新提案必須使用那個值
3. Accept 階段讓多數 Acceptor 接受值

**Emma**: 完全正確！這就是 Paxos 如何保證 Safety：一旦一個值被選定（多數接受），之後的提案都會提議同一個值。

---

## Act 5: Multi-Paxos

**場景**：討論如何將 Basic Paxos 應用到實際系統

**Sarah**: Basic Paxos 只能對單一值達成共識。如果我們要處理一系列的值（比如日誌），該怎麼辦？

**Emma**: 優秀的問題！這就引出了 Multi-Paxos。它的核心思想是：選出一個穩定的 Leader，讓它處理所有提案。

### Multi-Paxos 的優化

**Emma**: Basic Paxos 的問題是每個提案都需要兩輪 RPC（Prepare + Accept），延遲很高。Multi-Paxos 通過以下優化來解決：

```go
type MultiPaxosNode struct {
    id            string
    isLeader      bool
    leaderID      string
    currentTerm   int

    // 日誌
    log           []LogEntry
    commitIndex   int

    // Paxos 狀態
    minProposal   ProposalNumber
    acceptedLog   map[int]LogEntry // index -> LogEntry

    // Leader 狀態
    nextIndex     map[string]int
    matchIndex    map[string]int
}

type LogEntry struct {
    Index    int
    Term     int
    Proposal ProposalNumber
    Command  interface{}
}
```

**優化 1：跳過 Prepare 階段**

```go
// 如果節點是穩定的 Leader，可以直接發送 Accept
func (mp *MultiPaxosNode) ProposeCommand(cmd interface{}) error {
    if !mp.isLeader {
        return errors.New("not the leader")
    }

    // 生成新的日誌條目
    entry := LogEntry{
        Index:    len(mp.log),
        Term:     mp.currentTerm,
        Proposal: ProposalNumber{Number: mp.currentTerm, NodeID: mp.id},
        Command:  cmd,
    }

    mp.log = append(mp.log, entry)

    // 直接進入 Accept 階段（跳過 Prepare）
    return mp.sendAcceptForEntry(entry)
}

func (mp *MultiPaxosNode) sendAcceptForEntry(entry LogEntry) error {
    log.Printf("Leader %s proposing entry at index %d", mp.id, entry.Index)

    acceptedCount := 1 // Leader 自己算一個

    for _, peer := range mp.peers {
        go func(peer string) {
            resp := mp.sendAccept(peer, entry)

            if resp.Accepted {
                acceptedCount++

                if acceptedCount > len(mp.peers)/2 {
                    // 多數接受，可以提交
                    mp.commitIndex = entry.Index
                    mp.applyCommand(entry.Command)
                }
            } else if resp.NeedPrepare {
                // 如果 Acceptor 拒絕，退回到完整的 Paxos 流程
                mp.runFullPaxos(entry)
            }
        }(peer)
    }

    return nil
}
```

**David**: 什麼時候需要退回到完整的 Paxos 流程？

**Emma**: 當出現以下情況時：
1. 有其他節點也認為自己是 Leader（網路分區恢復後）
2. Acceptor 已經承諾了更高的提案編號

這時候就需要執行完整的 Prepare + Accept 流程。

**優化 2：批次處理**

```go
type Batch struct {
    entries []LogEntry
}

func (mp *MultiPaxosNode) BatchPropose(commands []interface{}) error {
    batch := Batch{}

    for _, cmd := range commands {
        entry := LogEntry{
            Index:    len(mp.log),
            Term:     mp.currentTerm,
            Proposal: ProposalNumber{Number: mp.currentTerm, NodeID: mp.id},
            Command:  cmd,
        }
        mp.log = append(mp.log, entry)
        batch.entries = append(batch.entries, entry)
    }

    // 一次性發送整個批次
    return mp.sendAcceptBatch(batch)
}
```

**優化 3：流水線處理**

```go
func (mp *MultiPaxosNode) PipelinePropose() {
    // 不等待前一個提案完成，連續發送多個提案
    for i := 0; i < 10; i++ {
        go mp.ProposeCommand(fmt.Sprintf("cmd-%d", i))
    }
}
```

**Michael**: 這看起來跟 Raft 很像啊？

**Emma**: 沒錯！實際上，Raft 可以看作是 Multi-Paxos 的一種特化實作：
- Raft 的 Term = Paxos 的 Proposal Number
- Raft 的 Leader Election = Multi-Paxos 的 Leader 選舉
- Raft 的 Log Replication = Multi-Paxos 的 Accept 階段

Raft 的創新在於：
1. 把 Paxos 的概念簡化成容易理解的「Leader、Term、Log」
2. 明確定義了狀態轉換規則
3. 增加了日誌完整性檢查

---

## Act 6: Raft vs Paxos 對比

**場景**：團隊討論選擇哪種演算法

**Sarah**: 既然 Raft 和 Paxos 這麼相似，我們該選哪個？

**Emma**: 讓我們列出它們的優缺點：

### Raft 的優勢

```
1. 易於理解
   - 狀態機模型清晰（Follower -> Candidate -> Leader）
   - 概念簡單（Term, Log, Commit）

2. 易於實作
   - 論文中包含詳細的實作指南
   - 許多開源實作（etcd, Consul, TiKV）

3. 強領導者模型
   - 所有寫入都經過 Leader
   - 日誌只從 Leader 流向 Follower（單向）

4. 成員變更
   - 論文中明確定義了 Joint Consensus 機制
```

**實際應用**：
- **etcd**: Kubernetes 的核心組件，使用 Raft
- **Consul**: 服務發現與配置管理
- **TiKV**: TiDB 的儲存引擎

### Paxos 的優勢

```
1. 理論優雅
   - 數學證明嚴謹
   - 更一般化的共識協議

2. 更靈活
   - 不要求強 Leader
   - 任何節點都可以提案

3. 更高的理論效能
   - 可以並發處理多個提案
   - 不需要 Leader 心跳

4. 變種豐富
   - Fast Paxos: 減少延遲
   - Cheap Paxos: 減少副本數
   - Byzantine Paxos: 容忍惡意節點
```

**實際應用**：
- **Chubby**: Google 的分散式鎖服務
- **Spanner**: Google 的全球分散式資料庫
- **Cassandra**: 使用類 Paxos 的共識

### 詳細對比表

| 維度 | Raft | Paxos |
|------|------|-------|
| **理解難度** | ★★☆☆☆ | ★★★★★ |
| **實作難度** | ★★★☆☆ | ★★★★★ |
| **正確性證明** | 較簡單 | 非常嚴謹 |
| **效能** | 中等（依賴 Leader） | 高（並發提案） |
| **延遲** | 2 RTT（正常情況） | 2 RTT（Multi-Paxos）|
| **吞吐量** | 高（批次 + 流水線） | 高（並發提案） |
| **成員變更** | 明確定義 | 需要額外機制 |
| **生態系統** | 豐富（etcd, Consul） | 較少開源實作 |

**David**: 所以對於大多數工程師，Raft 是更好的選擇？

**Emma**: 是的。Raft 的設計目標就是「可理解性」。除非你有特殊需求（比如需要 Byzantine 容錯，或者需要極致的效能），否則 Raft 是更實用的選擇。

### 性能對比實驗

```go
// 測試設定：5 節點，100,000 次寫入

// Raft
BenchmarkRaft-5   100000   1250 us/op   80,000 ops/sec
  - Leader 處理所有寫入
  - 批次大小：10
  - 流水線深度：100

// Multi-Paxos
BenchmarkMultiPaxos-5   100000   1180 us/op   84,746 ops/sec
  - 多個 Proposer 並發提案
  - 衝突率：5%

結論：效能相近，Paxos 略高（但實作複雜度高得多）
```

---

## Act 7: 實作共識系統的最佳實踐

**場景**：討論生產環境的注意事項

**Michael**: 如果我們要在生產環境部署 Raft 叢集，需要注意什麼？

**Emma**: 很好的問題！讓我分享一些最佳實踐。

### 1. 選舉超時調優

```go
const (
    // 選舉超時：150-300ms
    ElectionTimeoutMin = 150 * time.Millisecond
    ElectionTimeoutMax = 300 * time.Millisecond

    // 心跳間隔：選舉超時的 1/10
    HeartbeatInterval = 50 * time.Millisecond
)

// 根據網路延遲動態調整
func (rn *RaftNode) adjustTimeout(latency time.Duration) {
    // 選舉超時應該是網路延遲的 10-20 倍
    minTimeout := latency * 10
    maxTimeout := latency * 20

    rn.electionTimeoutMin = minTimeout
    rn.electionTimeoutMax = maxTimeout
    rn.heartbeatInterval = minTimeout / 3
}
```

**為什麼這些值很重要**：
- 太小：頻繁觸發不必要的選舉（假陽性）
- 太大：故障恢復時間長（Leader 掛掉後才發現）

### 2. 日誌壓縮與快照

```go
type Snapshot struct {
    LastIncludedIndex int         // 快照包含的最後一個日誌索引
    LastIncludedTerm  int         // 最後一個日誌的 Term
    StateMachine      []byte      // 狀態機的序列化資料
    Metadata          SnapshotMeta
}

func (rn *RaftNode) CreateSnapshot() error {
    // 當日誌增長到一定大小，建立快照
    if len(rn.log) < 10000 {
        return nil
    }

    // 序列化狀態機
    stateData, err := rn.stateMachine.Serialize()
    if err != nil {
        return err
    }

    snapshot := Snapshot{
        LastIncludedIndex: rn.lastApplied,
        LastIncludedTerm:  rn.log[rn.lastApplied].Term,
        StateMachine:      stateData,
    }

    // 持久化快照
    err = rn.storage.SaveSnapshot(snapshot)
    if err != nil {
        return err
    }

    // 截斷日誌
    rn.log = rn.log[rn.lastApplied+1:]

    log.Printf("Node %s created snapshot up to index %d", rn.id, rn.lastApplied)
    return nil
}

// 發送快照給落後的 Follower
func (rn *RaftNode) sendSnapshot(peer string) error {
    snapshot, err := rn.storage.LoadSnapshot()
    if err != nil {
        return err
    }

    req := &InstallSnapshotRequest{
        Term:              rn.currentTerm,
        LeaderID:          rn.id,
        LastIncludedIndex: snapshot.LastIncludedIndex,
        LastIncludedTerm:  snapshot.LastIncludedTerm,
        Data:              snapshot.StateMachine,
    }

    resp := rn.sendInstallSnapshot(peer, req)

    if resp.Success {
        rn.nextIndex[peer] = snapshot.LastIncludedIndex + 1
    }

    return nil
}
```

### 3. 預投票（Pre-Vote）機制

```go
// 問題：網路分區的節點不斷遞增 term，恢復後會打斷穩定的 Leader

// 解決方案：增加 Pre-Vote 階段
func (rn *RaftNode) startPreVote() {
    // 先詢問其他節點是否願意投票（不增加 term）
    preVoteTerm := rn.currentTerm + 1
    votesReceived := 1

    for _, peer := range rn.peers {
        go func(peer string) {
            req := &PreVoteRequest{
                Term:         preVoteTerm,
                CandidateID:  rn.id,
                LastLogIndex: len(rn.log) - 1,
                LastLogTerm:  rn.getLastLogTerm(),
            }

            resp := rn.sendPreVote(peer, req)

            if resp.VoteGranted {
                votesReceived++

                // 只有在 Pre-Vote 成功時，才真正發起選舉
                if votesReceived > len(rn.peers)/2 {
                    rn.startElection()
                }
            }
        }(peer)
    }
}
```

### 4. 讀取優化：ReadIndex

```go
// 問題：讀取也要走 Raft 日誌嗎？太慢了！

// 解決方案：ReadIndex 機制
func (rn *RaftNode) ReadIndex() (int, error) {
    if rn.state != Leader {
        return 0, errors.New("not the leader")
    }

    readIndex := rn.commitIndex

    // 發送心跳確認自己仍是 Leader
    ackCount := 1
    for _, peer := range rn.peers {
        resp := rn.sendHeartbeat(peer)
        if resp.Success {
            ackCount++
        }
    }

    // 多數節點確認，可以安全地讀取
    if ackCount > len(rn.peers)/2 {
        return readIndex, nil
    }

    return 0, errors.New("failed to confirm leadership")
}

// 客戶端讀取流程
func (rn *RaftNode) Read(key string) (string, error) {
    readIndex, err := rn.ReadIndex()
    if err != nil {
        return "", err
    }

    // 等待狀態機應用到 readIndex
    for rn.lastApplied < readIndex {
        time.Sleep(10 * time.Millisecond)
    }

    // 現在可以安全地讀取
    return rn.stateMachine.Get(key), nil
}
```

### 5. 成員變更：Joint Consensus

```go
// 問題：直接從 {A,B,C} 切換到 {A,B,D} 可能導致腦裂

// 解決方案：使用 Joint Consensus (C_old,new)
func (rn *RaftNode) ChangeMembership(newMembers []string) error {
    // 階段 1：加入 Joint Consensus 配置
    jointConfig := Configuration{
        Old: rn.currentMembers,
        New: newMembers,
    }

    // 將 C_old,new 作為日誌條目提交
    entry := LogEntry{
        Index:   len(rn.log),
        Term:    rn.currentTerm,
        Command: jointConfig,
    }

    err := rn.replicateEntry(entry)
    if err != nil {
        return err
    }

    // 在 C_old,new 階段，決策需要兩邊都達到多數
    // Old: 2/3,  New: 2/3

    // 階段 2：提交 C_new 配置
    newConfig := Configuration{
        Old: nil,
        New: newMembers,
    }

    entry = LogEntry{
        Index:   len(rn.log),
        Term:    rn.currentTerm,
        Command: newConfig,
    }

    return rn.replicateEntry(entry)
}

func (rn *RaftNode) hasMajority(ackCount int, config Configuration) bool {
    if config.Old != nil {
        // Joint Consensus：兩邊都要多數
        oldMajority := ackCount > len(config.Old)/2
        newMajority := ackCount > len(config.New)/2
        return oldMajority && newMajority
    } else {
        // 普通配置：單一多數
        return ackCount > len(config.New)/2
    }
}
```

**Emma**: 這就是生產級 Raft 實作需要考慮的問題。etcd 和 Consul 都實作了這些優化。

**David**: 我明白了。共識演算法不僅是理論，還有很多工程細節。

**Sarah**: 我們今天學到了很多！Raft 和Paxos 各有優勢，但對大多數應用來說，Raft 是更好的選擇。

**Michael**: 我現在更有信心部署高可用的分散式系統了。理解了共識演算法，就能更好地排查 etcd 或 Consul 的問題。

**Emma**: 完全正確！共識演算法是分散式系統的基石。掌握了它，你就打開了理解所有分散式系統的大門。

---

## 總結

### Raft 核心概念

1. **角色**: Follower、Candidate、Leader
2. **Term**: 邏輯時鐘，用於檢測過時資訊
3. **Leader Election**: 隨機超時 + 多數投票
4. **Log Replication**: Leader 複製日誌到多數節點
5. **Safety**: 已提交的日誌不會丟失

### Paxos 核心概念

1. **角色**: Proposer、Acceptor、Learner
2. **兩階段**: Prepare（承諾）+ Accept（接受）
3. **提案編號**: 用於排序和拒絕舊提案
4. **共識不變性**: 一旦值被選定，不能改變
5. **Multi-Paxos**: 選出穩定 Leader，跳過 Prepare

### 選擇建議

- **易用性優先**: 選 Raft
- **理論研究**: 學習 Paxos
- **生產環境**: 使用 etcd (Raft) 或 Chubby (Paxos)
- **特殊需求**: 考慮 Paxos 變種

下一章我們將學習 **Time-Series Database**，看看共識演算法如何應用在高效能時序資料庫中！
