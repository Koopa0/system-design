package internal

import (
	"encoding/json"
	"fmt"
	"sync"
)

// VectorClock 向量時鐘
type VectorClock struct {
	// 節點 ID → 版本號
	Clocks map[string]int `json:"clocks"`
	mu     sync.RWMutex
}

// NewVectorClock 創建向量時鐘
func NewVectorClock() *VectorClock {
	return &VectorClock{
		Clocks: make(map[string]int),
	}
}

// Increment 增加本節點的版本號
func (vc *VectorClock) Increment(nodeID string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.Clocks[nodeID]++
}

// Merge 合並兩個向量時鐘（取每個節點的最大值）
func (vc *VectorClock) Merge(other *VectorClock) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for nodeID, version := range other.Clocks {
		if vc.Clocks[nodeID] < version {
			vc.Clocks[nodeID] = version
		}
	}
}

// Compare 比較兩個向量時鐘
// 返回值：
//   - "equal": 相等
//   - "before": vc 發生在 other 之前
//   - "after": vc 發生在 other 之後
//   - "concurrent": 並發衝突
func (vc *VectorClock) Compare(other *VectorClock) string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	// 檢查 vc 是否 <= other (所有維度)
	vcLessOrEqual := true
	vcGreaterOrEqual := true

	// 獲取所有節點
	allNodes := make(map[string]bool)
	for node := range vc.Clocks {
		allNodes[node] = true
	}
	for node := range other.Clocks {
		allNodes[node] = true
	}

	// 比較每個維度
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
		return "equal" // 相等
	} else if vcLessOrEqual {
		return "before" // vc 發生在 other 之前
	} else if vcGreaterOrEqual {
		return "after" // vc 發生在 other 之後
	} else {
		return "concurrent" // 並發衝突
	}
}

// Copy 複製向量時鐘
func (vc *VectorClock) Copy() *VectorClock {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	newVC := NewVectorClock()
	for nodeID, version := range vc.Clocks {
		newVC.Clocks[nodeID] = version
	}

	return newVC
}

// String 字符串表示
func (vc *VectorClock) String() string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	return fmt.Sprintf("%v", vc.Clocks)
}

// ToJSON 轉換為 JSON
func (vc *VectorClock) ToJSON() ([]byte, error) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	return json.Marshal(vc.Clocks)
}

// FromJSON 從 JSON 創建
func FromJSON(data []byte) (*VectorClock, error) {
	vc := NewVectorClock()

	if err := json.Unmarshal(data, &vc.Clocks); err != nil {
		return nil, err
	}

	return vc, nil
}

// Version 數據版本（包含值和向量時鐘）
type Version struct {
	Value       []byte       `json:"value"`
	VectorClock *VectorClock `json:"vector_clock"`
}

// NewVersion 創建新版本
func NewVersion(value []byte, vectorClock *VectorClock) *Version {
	return &Version{
		Value:       value,
		VectorClock: vectorClock,
	}
}

// VersionedValue 帶版本的值（可能有多個並發版本）
type VersionedValue struct {
	Versions []*Version
	mu       sync.RWMutex
}

// NewVersionedValue 創建帶版本的值
func NewVersionedValue() *VersionedValue {
	return &VersionedValue{
		Versions: make([]*Version, 0),
	}
}

// AddVersion 添加版本
func (vv *VersionedValue) AddVersion(version *Version) {
	vv.mu.Lock()
	defer vv.mu.Unlock()

	vv.Versions = append(vv.Versions, version)
}

// Reconcile 調和版本（去除被覆蓋的舊版本）
func (vv *VersionedValue) Reconcile() []*Version {
	vv.mu.Lock()
	defer vv.mu.Unlock()

	if len(vv.Versions) <= 1 {
		return vv.Versions
	}

	result := make([]*Version, 0)

	for _, v1 := range vv.Versions {
		obsolete := false

		for _, v2 := range vv.Versions {
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

	vv.Versions = result
	return result
}

// GetLatest 獲取最新版本（如果有多個並發版本，返回第一個）
func (vv *VersionedValue) GetLatest() *Version {
	vv.mu.RLock()
	defer vv.mu.RUnlock()

	versions := vv.Reconcile()

	if len(versions) == 0 {
		return nil
	}

	return versions[0]
}

// HasConflict 檢查是否有並發衝突
func (vv *VersionedValue) HasConflict() bool {
	vv.mu.RLock()
	defer vv.mu.RUnlock()

	versions := vv.Reconcile()
	return len(versions) > 1
}
