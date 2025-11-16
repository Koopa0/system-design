package internal

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// QuorumConfig Quorum 配置
type QuorumConfig struct {
	N int // 副本數量
	W int // 寫 Quorum
	R int // 讀 Quorum
}

// DistributedKVStore 分布式 KV Store
type DistributedKVStore struct {
	nodeID         string
	nodeAddr       string
	consistentHash *ConsistentHash
	quorumConfig   *QuorumConfig
	gossip         *GossipProtocol
	localData      map[string]*VersionedValue // 本地數據存儲
	mu             sync.RWMutex
}

// NewDistributedKVStore 創建分布式 KV Store
func NewDistributedKVStore(nodeID, nodeAddr string, config *QuorumConfig) *DistributedKVStore {
	kvStore := &DistributedKVStore{
		nodeID:         nodeID,
		nodeAddr:       nodeAddr,
		consistentHash: NewConsistentHash(150), // 每個節點 150 個虛擬節點
		quorumConfig:   config,
		gossip:         NewGossipProtocol(nodeID, nodeAddr),
		localData:      make(map[string]*VersionedValue),
	}

	// 設置 Gossip 回調
	kvStore.gossip.SetCallbacks(
		func(addr string) {
			// 節點添加
			kvStore.consistentHash.AddNode(addr)
			log.Printf("[KVStore] Node added to hash ring: %s", addr)
		},
		func(addr string) {
			// 節點移除
			kvStore.consistentHash.RemoveNode(addr)
			log.Printf("[KVStore] Node removed from hash ring: %s", addr)
		},
	)

	// 添加本地節點到哈希環
	kvStore.consistentHash.AddNode(nodeAddr)

	return kvStore
}

// Start 啟動 KV Store
func (kv *DistributedKVStore) Start() {
	kv.gossip.Start()
	log.Printf("[KVStore] Started on node %s (%s)", kv.nodeID, kv.nodeAddr)
}

// Stop 停止 KV Store
func (kv *DistributedKVStore) Stop() {
	kv.gossip.Stop()
}

// AddSeedNode 添加種子節點
func (kv *DistributedKVStore) AddSeedNode(nodeID, addr string) {
	kv.gossip.AddKnownNode(nodeID, addr)
	kv.consistentHash.AddNode(addr)
}

// Set 寫入數據（Quorum 寫入）
func (kv *DistributedKVStore) Set(key string, value []byte) error {
	// 1. 找到負責這個 key 的 N 個副本節點
	replicaNodes := kv.getReplicaNodes(key, kv.quorumConfig.N)

	if len(replicaNodes) == 0 {
		return fmt.Errorf("no replica nodes available")
	}

	// 2. 創建新版本（帶向量時鐘）
	vectorClock := NewVectorClock()

	// 如果本地有舊版本，合並向量時鐘
	kv.mu.RLock()
	if vv, exists := kv.localData[key]; exists {
		for _, v := range vv.Versions {
			vectorClock.Merge(v.VectorClock)
		}
	}
	kv.mu.RUnlock()

	// 增加本節點的版本號
	vectorClock.Increment(kv.nodeID)

	newVersion := NewVersion(value, vectorClock)

	// 3. 並發寫入所有副本
	successCh := make(chan bool, len(replicaNodes))
	errorCh := make(chan error, len(replicaNodes))

	for _, node := range replicaNodes {
		go func(nodeAddr string) {
			err := kv.writeToNode(nodeAddr, key, newVersion)
			if err != nil {
				errorCh <- err
			} else {
				successCh <- true
			}
		}(node)
	}

	// 4. 等待 W 個副本成功
	successCount := 0
	timeout := time.After(5 * time.Second)

	for i := 0; i < len(replicaNodes); i++ {
		select {
		case <-successCh:
			successCount++
			if successCount >= kv.quorumConfig.W {
				log.Printf("[KVStore] Set key=%s, W=%d/%d success", key, successCount, kv.quorumConfig.W)
				return nil // 達到 W，寫入成功
			}
		case err := <-errorCh:
			// 記錄錯誤，但繼續等待
			log.Printf("[KVStore] Write error: %v", err)
		case <-timeout:
			return fmt.Errorf("quorum write timeout: only %d/%d writes succeeded",
				successCount, kv.quorumConfig.W)
		}
	}

	// 5. 沒有達到 W 個成功
	return fmt.Errorf("quorum not met: only %d/%d writes succeeded",
		successCount, kv.quorumConfig.W)
}

// Get 讀取數據（Quorum 讀取）
func (kv *DistributedKVStore) Get(key string) ([]byte, error) {
	// 1. 找到負責這個 key 的 N 個副本節點
	replicaNodes := kv.getReplicaNodes(key, kv.quorumConfig.N)

	if len(replicaNodes) == 0 {
		return nil, fmt.Errorf("no replica nodes available")
	}

	// 2. 並發讀取 R 個副本
	readCount := kv.quorumConfig.R
	if readCount > len(replicaNodes) {
		readCount = len(replicaNodes)
	}

	type ReadResult struct {
		Versions []*Version
		Error    error
	}

	resultCh := make(chan *ReadResult, readCount)

	for i := 0; i < readCount; i++ {
		go func(nodeAddr string) {
			versions, err := kv.readFromNode(nodeAddr, key)
			resultCh <- &ReadResult{
				Versions: versions,
				Error:    err,
			}
		}(replicaNodes[i])
	}

	// 3. 收集 R 個結果
	allVersions := make([]*Version, 0)
	timeout := time.After(5 * time.Second)
	successCount := 0

	for i := 0; i < readCount; i++ {
		select {
		case result := <-resultCh:
			if result.Error == nil {
				allVersions = append(allVersions, result.Versions...)
				successCount++
			}
		case <-timeout:
			break
		}
	}

	if successCount < kv.quorumConfig.R {
		return nil, fmt.Errorf("quorum read failed: only %d/%d reads succeeded",
			successCount, kv.quorumConfig.R)
	}

	if len(allVersions) == 0 {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	// 4. 調和版本，選擇最新的
	vv := NewVersionedValue()
	for _, v := range allVersions {
		vv.AddVersion(v)
	}

	reconciledVersions := vv.Reconcile()

	if len(reconciledVersions) == 0 {
		return nil, fmt.Errorf("no valid versions after reconciliation")
	}

	// 5. 如果有衝突，返回第一個版本（簡化處理）
	if len(reconciledVersions) > 1 {
		log.Printf("[KVStore] Conflict detected for key=%s, %d versions", key, len(reconciledVersions))
	}

	// 6. Read Repair（異步修復過時的副本）
	go kv.readRepair(key, reconciledVersions, replicaNodes)

	return reconciledVersions[0].Value, nil
}

// Delete 刪除數據
func (kv *DistributedKVStore) Delete(key string) error {
	// 寫入空值（墓碑標記）
	return kv.Set(key, []byte{})
}

// getReplicaNodes 獲取副本節點（使用一致性哈希）
func (kv *DistributedKVStore) getReplicaNodes(key string, count int) []string {
	return kv.consistentHash.GetNodes(key, count)
}

// writeToNode 寫入到指定節點
func (kv *DistributedKVStore) writeToNode(nodeAddr string, key string, version *Version) error {
	// 如果是本地節點，直接寫入
	if nodeAddr == kv.nodeAddr {
		kv.mu.Lock()
		defer kv.mu.Unlock()

		vv, exists := kv.localData[key]
		if !exists {
			vv = NewVersionedValue()
			kv.localData[key] = vv
		}

		vv.AddVersion(version)
		return nil
	}

	// 否則通過網絡發送（實際應用中使用 HTTP/gRPC）
	// 為了簡化，這裡模擬成功
	return nil
}

// readFromNode 從指定節點讀取
func (kv *DistributedKVStore) readFromNode(nodeAddr string, key string) ([]*Version, error) {
	// 如果是本地節點，直接讀取
	if nodeAddr == kv.nodeAddr {
		kv.mu.RLock()
		defer kv.mu.RUnlock()

		vv, exists := kv.localData[key]
		if !exists {
			return nil, fmt.Errorf("key not found")
		}

		return vv.Versions, nil
	}

	// 否則通過網絡讀取（實際應用中使用 HTTP/gRPC）
	// 為了簡化，這裡返回空
	return nil, fmt.Errorf("remote read not implemented")
}

// readRepair Read Repair：異步修復過時的副本
func (kv *DistributedKVStore) readRepair(key string, latestVersions []*Version, replicaNodes []string) {
	for _, nodeAddr := range replicaNodes {
		// 讀取每個副本的版本
		versions, err := kv.readFromNode(nodeAddr, key)
		if err != nil {
			continue
		}

		// 檢查是否需要修復
		needRepair := false
		for _, nodeVersion := range versions {
			obsolete := false
			for _, latestVersion := range latestVersions {
				if nodeVersion.VectorClock.Compare(latestVersion.VectorClock) == "before" {
					obsolete = true
					break
				}
			}

			if obsolete {
				needRepair = true
				break
			}
		}

		if needRepair {
			log.Printf("[KVStore] Read repair: updating stale replica on %s for key %s", nodeAddr, key)

			// 寫入最新版本
			for _, latestVersion := range latestVersions {
				kv.writeToNode(nodeAddr, key, latestVersion)
			}
		}
	}
}

// GetStats 獲取統計數據
func (kv *DistributedKVStore) GetStats() map[string]interface{} {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	totalKeys := len(kv.localData)
	totalVersions := 0
	conflictKeys := 0

	for _, vv := range kv.localData {
		totalVersions += len(vv.Versions)
		if len(vv.Reconcile()) > 1 {
			conflictKeys++
		}
	}

	return map[string]interface{}{
		"node_id":             kv.nodeID,
		"node_addr":           kv.nodeAddr,
		"total_keys":          totalKeys,
		"total_versions":      totalVersions,
		"conflict_keys":       conflictKeys,
		"quorum_config":       kv.quorumConfig,
		"consistent_hash":     kv.consistentHash.GetStats(),
		"gossip_known_nodes":  len(kv.gossip.GetAllNodes()),
		"gossip_alive_nodes":  len(kv.gossip.GetAliveNodes()),
	}
}

// ExportData 導出本地數據（用於調試）
func (kv *DistributedKVStore) ExportData() map[string]interface{} {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	data := make(map[string]interface{})

	for key, vv := range kv.localData {
		versions := make([]map[string]interface{}, 0)

		for _, v := range vv.Versions {
			versions = append(versions, map[string]interface{}{
				"value":        string(v.Value),
				"vector_clock": v.VectorClock.String(),
			})
		}

		data[key] = versions
	}

	return data
}

// MarshalJSON 自定義 JSON 序列化
func (qc *QuorumConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]int{
		"N": qc.N,
		"W": qc.W,
		"R": qc.R,
	})
}
