// Package cache 實作多種快取淘汰演算法。
package cache

import (
	"container/list"
	"sync"
)

// LRU 實作 Least Recently Used 快取淘汰演算法。
//
// 演算法原理：
//   最近最少使用的資料最先被淘汰
//   使用雙向鏈結串列 + HashMap 實作
//
// 資料結構：
//   - 雙向鏈結串列：維護存取順序（頭部為最近使用）
//   - HashMap：快速查找（O(1) 時間複雜度）
//
// 時間複雜度：
//   - Get: O(1)
//   - Put: O(1)
//   - 淘汰: O(1)
//
// 空間複雜度：O(n)，n 為容量
//
// 適用場景：
//   - 熱點資料快取
//   - 大部分場景的預設選擇
//   - 假設：最近使用的資料未來還會被使用
//
// 優點：
//   - 實作簡單
//   - 效能優秀（O(1) 操作）
//   - 命中率穩定
//
// 缺點：
//   - 無法處理突發流量（一次性大量存取會污染快取）
//   - 不考慮存取頻率（只看最近性）
type LRU struct {
	capacity int                        // 容量
	cache    map[string]*list.Element   // key -> 鏈表節點
	list     *list.List                 // 雙向鏈結串列
	mu       sync.RWMutex              // 讀寫鎖
}

// entry 是鏈表節點儲存的資料。
type entry struct {
	key   string
	value interface{}
}

// NewLRU 建立新的 LRU 快取。
//
// 參數：
//   capacity: 快取容量（最多儲存多少項目）
//
// 實作細節：
//   - 使用 container/list 作為雙向鏈結串列
//   - 鏈表頭部是最近使用的項目
//   - 鏈表尾部是最久未使用的項目
func NewLRU(capacity int) *LRU {
	return &LRU{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get 取得快取值。
//
// 參數：
//   key: 快取鍵
//
// 返回：
//   value: 快取值
//   ok: 是否命中
//
// 行為：
//   命中時，將該項目移到鏈表頭部（標記為最近使用）
func (lru *LRU) Get(key string) (interface{}, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.cache[key]; ok {
		// 移到鏈表頭部（最近使用）
		lru.list.MoveToFront(elem)
		return elem.Value.(*entry).value, true
	}

	return nil, false
}

// Put 設定快取值。
//
// 參數：
//   key: 快取鍵
//   value: 快取值
//
// 行為：
//   1. 如果 key 已存在，更新值並移到頭部
//   2. 如果 key 不存在：
//      - 容量未滿：直接新增到頭部
//      - 容量已滿：移除尾部項目（最久未使用），再新增到頭部
//
// 淘汰策略：
//   當容量滿時，淘汰鏈表尾部的項目（最久未使用）
func (lru *LRU) Set(key string, value interface{}) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	// 如果 key 已存在，更新值
	if elem, ok := lru.cache[key]; ok {
		lru.list.MoveToFront(elem)
		elem.Value.(*entry).value = value
		return
	}

	// 新增項目
	elem := lru.list.PushFront(&entry{key: key, value: value})
	lru.cache[key] = elem

	// 檢查容量，超過則淘汰
	if lru.list.Len() > lru.capacity {
		lru.evict()
	}
}

// evict 淘汰最久未使用的項目。
//
// 實作細節：
//   移除鏈表尾部的項目
//   同時從 HashMap 中刪除
func (lru *LRU) evict() {
	elem := lru.list.Back()
	if elem != nil {
		lru.list.Remove(elem)
		delete(lru.cache, elem.Value.(*entry).key)
	}
}

// Delete 刪除快取項目。
func (lru *LRU) Delete(key string) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.cache[key]; ok {
		lru.list.Remove(elem)
		delete(lru.cache, key)
	}
}

// Len 返回當前快取項目數量。
func (lru *LRU) Len() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return lru.list.Len()
}

// Clear 清空快取。
func (lru *LRU) Clear() {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	lru.cache = make(map[string]*list.Element)
	lru.list = list.New()
}

// Keys 返回所有快取鍵（從最近到最久）。
//
// 用途：
//   監控、除錯、測試
func (lru *LRU) Keys() []string {
	lru.mu.RLock()
	defer lru.mu.RUnlock()

	keys := make([]string, 0, lru.list.Len())
	for elem := lru.list.Front(); elem != nil; elem = elem.Next() {
		keys = append(keys, elem.Value.(*entry).key)
	}
	return keys
}
