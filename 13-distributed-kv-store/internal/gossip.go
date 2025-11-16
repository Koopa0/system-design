package internal

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

// NodeInfo 節點信息
type NodeInfo struct {
	ID        string    `json:"id"`
	Addr      string    `json:"addr"`
	Status    string    `json:"status"` // "alive", "suspected", "dead"
	Heartbeat int64     `json:"heartbeat"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GossipProtocol Gossip 協議
type GossipProtocol struct {
	localNode      *NodeInfo
	knownNodes     map[string]*NodeInfo // 所有已知節點
	mu             sync.RWMutex
	gossipInterval time.Duration // gossip 間隔（例如 1 秒）
	fanout         int           // 每次 gossip 的節點數（例如 3）
	stopChan       chan bool
	onNodeAdded    func(string)  // 節點添加回調
	onNodeRemoved  func(string)  // 節點移除回調
}

// NewGossipProtocol 創建 Gossip 協議
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
		stopChan:       make(chan bool),
	}
}

// SetCallbacks 設置回調函數
func (gp *GossipProtocol) SetCallbacks(onNodeAdded, onNodeRemoved func(string)) {
	gp.onNodeAdded = onNodeAdded
	gp.onNodeRemoved = onNodeRemoved
}

// AddKnownNode 手動添加已知節點（用於啟動時的種子節點）
func (gp *GossipProtocol) AddKnownNode(nodeID, addr string) {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	if nodeID == gp.localNode.ID {
		return // 不添加自己
	}

	gp.knownNodes[nodeID] = &NodeInfo{
		ID:        nodeID,
		Addr:      addr,
		Status:    "alive",
		Heartbeat: 0,
		UpdatedAt: time.Now(),
	}

	if gp.onNodeAdded != nil {
		gp.onNodeAdded(addr)
	}
}

// Start 啟動 Gossip 協議
func (gp *GossipProtocol) Start() {
	// 定期增加本地心跳
	go gp.incrementHeartbeat()

	// 定期 gossip
	go gp.gossipLoop()

	// 定期檢測故障
	go gp.detectFailures()

	log.Printf("[Gossip] Started for node %s (%s)", gp.localNode.ID, gp.localNode.Addr)
}

// Stop 停止 Gossip 協議
func (gp *GossipProtocol) Stop() {
	close(gp.stopChan)
}

// incrementHeartbeat 增加本地心跳
func (gp *GossipProtocol) incrementHeartbeat() {
	ticker := time.NewTicker(gp.gossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			gp.mu.Lock()
			gp.localNode.Heartbeat++
			gp.localNode.UpdatedAt = time.Now()
			gp.mu.Unlock()
		case <-gp.stopChan:
			return
		}
	}
}

// gossipLoop Gossip 循環
func (gp *GossipProtocol) gossipLoop() {
	ticker := time.NewTicker(gp.gossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			gp.gossip()
		case <-gp.stopChan:
			return
		}
	}
}

// gossip 執行一輪 gossip
func (gp *GossipProtocol) gossip() {
	gp.mu.RLock()

	// 隨機選擇 fanout 個節點
	targets := gp.selectRandomNodes(gp.fanout)

	// 準備要發送的數據（所有已知節點的信息）
	gossipData := gp.prepareGossipData()

	gp.mu.RUnlock()

	// 模擬發送 gossip 消息（實際應用中通過網絡發送）
	for _, target := range targets {
		gp.sendGossip(target, gossipData)
	}
}

// selectRandomNodes 選擇隨機節點
func (gp *GossipProtocol) selectRandomNodes(count int) []*NodeInfo {
	nodes := make([]*NodeInfo, 0, len(gp.knownNodes))

	for _, node := range gp.knownNodes {
		if node.ID != gp.localNode.ID && node.Status == "alive" {
			nodes = append(nodes, node)
		}
	}

	if len(nodes) == 0 {
		return []*NodeInfo{}
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

// prepareGossipData 準備 gossip 數據
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

// sendGossip 發送 gossip 消息（模擬）
func (gp *GossipProtocol) sendGossip(target *NodeInfo, data map[string]*NodeInfo) {
	// 實際應用中，這裡應該通過 HTTP/gRPC 發送
	// 為了簡化，這裡模擬接收對方的響應

	// 模擬網絡延遲
	// time.Sleep(10 * time.Millisecond)

	// 模擬對方也發送回它知道的節點信息
	// response := ...

	// 合並對方的信息
	// gp.mergeGossipData(response)
}

// MergeGossipData 合並 gossip 數據（公開方法，供網絡層調用）
func (gp *GossipProtocol) MergeGossipData(incomingData map[string]*NodeInfo) {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	for nodeID, incomingNode := range incomingData {
		if nodeID == gp.localNode.ID {
			continue // 跳過自己
		}

		existingNode, exists := gp.knownNodes[nodeID]

		if !exists {
			// 新節點，直接添加
			gp.knownNodes[nodeID] = incomingNode

			if gp.onNodeAdded != nil && incomingNode.Status == "alive" {
				gp.onNodeAdded(incomingNode.Addr)
			}
		} else {
			// 已知節點，比較心跳
			if incomingNode.Heartbeat > existingNode.Heartbeat {
				// 接收到更新的信息
				oldStatus := existingNode.Status
				gp.knownNodes[nodeID] = incomingNode

				// 如果狀態變化，觸發回調
				if oldStatus == "dead" && incomingNode.Status == "alive" {
					if gp.onNodeAdded != nil {
						gp.onNodeAdded(incomingNode.Addr)
					}
				}
			}
		}
	}
}

// detectFailures 檢測故障
func (gp *GossipProtocol) detectFailures() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			gp.mu.Lock()

			now := time.Now()
			suspectThreshold := 10 * time.Second // 10 秒沒更新 → suspected
			deadThreshold := 30 * time.Second    // 30 秒沒更新 → dead

			for nodeID, node := range gp.knownNodes {
				if node.ID == gp.localNode.ID {
					continue
				}

				timeSinceUpdate := now.Sub(node.UpdatedAt)

				if timeSinceUpdate > deadThreshold {
					if node.Status != "dead" {
						log.Printf("[Gossip] Node %s marked as DEAD", nodeID)
						node.Status = "dead"

						if gp.onNodeRemoved != nil {
							gp.onNodeRemoved(node.Addr)
						}
					}
				} else if timeSinceUpdate > suspectThreshold {
					if node.Status == "alive" {
						log.Printf("[Gossip] Node %s marked as SUSPECTED", nodeID)
						node.Status = "suspected"
					}
				}
			}

			gp.mu.Unlock()
		case <-gp.stopChan:
			return
		}
	}
}

// GetAliveNodes 獲取所有存活節點
func (gp *GossipProtocol) GetAliveNodes() []*NodeInfo {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	alive := make([]*NodeInfo, 0)

	// 包含本地節點
	alive = append(alive, gp.localNode)

	// 包含其他存活節點
	for _, node := range gp.knownNodes {
		if node.Status == "alive" {
			alive = append(alive, node)
		}
	}

	return alive
}

// GetAllNodes 獲取所有已知節點
func (gp *GossipProtocol) GetAllNodes() []*NodeInfo {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	all := make([]*NodeInfo, 0)

	// 包含本地節點
	all = append(all, gp.localNode)

	// 包含所有已知節點
	for _, node := range gp.knownNodes {
		all = append(all, node)
	}

	return all
}

// GetLocalNode 獲取本地節點信息
func (gp *GossipProtocol) GetLocalNode() *NodeInfo {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	return gp.localNode
}
