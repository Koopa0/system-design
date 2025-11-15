// Package internal 實現時間輪算法（Timing Wheel）
//
// 時間輪算法：
//   - 經典調度算法，Netty、Kafka 都在使用
//   - O(1) 插入任務、O(1) 觸發任務
//   - 高性能、記憶體高效
//
// 核心概念：
//   - 圓形槽位數組（類似時鐘）
//   - 指針定時轉動（如秒針）
//   - 任務按時間分散在槽位中
package internal

import (
	"container/list"
	"sync"
	"time"
)

const (
	// SlotCount 槽位數量（3600 = 1 小時精度，每秒一個槽位）
	SlotCount = 3600

	// TickDuration 指針轉動間隔（1 秒）
	TickDuration = 1 * time.Second
)

// Task 調度任務
type Task struct {
	ID        string                 // 任務 ID
	ExecuteAt time.Time              // 執行時間
	Round     int                    // 需要轉幾圈
	Callback  string                 // 回調 URL
	Data      map[string]interface{} // 任務數據
	Retry     int                    // 重試次數
}

// TimingWheel 時間輪
//
// 算法說明：
//   圓形槽位數組，指針每秒轉動一格
//
//   Slot 0   →  [Task A, Task B]
//   Slot 1   →  []
//   Slot 2   →  [Task C]
//   ...
//   Slot 30  →  [Task D]  ← 30 秒後執行
//   ...
//   Slot 3599 → [Task E]
//            ↑ 當前指針
//
// 插入任務：O(1)
//   slot = (currentSlot + delaySeconds) % 3600
//   round = delaySeconds / 3600
//   wheel[slot].append(task)
//
// 觸發任務：O(1)
//   每秒檢查當前槽位
//   if task.round == 0: execute(task)
//   else: task.round--
type TimingWheel struct {
	slots       [SlotCount]*list.List // 槽位數組
	currentSlot int                   // 當前指針位置
	ticker      *time.Ticker          // 定時器
	mu          sync.RWMutex          // 讀寫鎖
	onTick      func([]*Task)         // 觸發回調
}

// NewTimingWheel 創建時間輪
func NewTimingWheel(onTick func([]*Task)) *TimingWheel {
	tw := &TimingWheel{
		currentSlot: 0,
		ticker:      time.NewTicker(TickDuration),
		onTick:      onTick,
	}

	// 初始化槽位
	for i := 0; i < SlotCount; i++ {
		tw.slots[i] = list.New()
	}

	return tw
}

// AddTask 添加任務到時間輪
//
// 算法：
//   1. 計算延遲秒數 = executeAt - now
//   2. 計算槽位 = (currentSlot + delaySeconds) % 3600
//   3. 計算圈數 = delaySeconds / 3600
//   4. 插入到槽位鏈表
//
// 時間複雜度：O(1)
func (tw *TimingWheel) AddTask(task *Task) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	// 1. 計算延遲
	delay := int(time.Until(task.ExecuteAt).Seconds())
	if delay < 0 {
		delay = 0 // 已過期，立即執行
	}

	// 2. 計算槽位和圈數
	slot := (tw.currentSlot + delay) % SlotCount
	task.Round = delay / SlotCount

	// 3. 插入槽位
	tw.slots[slot].PushBack(task)
}

// Start 啟動時間輪
func (tw *TimingWheel) Start() {
	go tw.run()
}

// run 時間輪主循環
func (tw *TimingWheel) run() {
	for range tw.ticker.C {
		tw.tick()
	}
}

// tick 指針轉動一格
//
// 算法：
//   1. 指針前進：currentSlot = (currentSlot + 1) % 3600
//   2. 檢查當前槽位的所有任務
//   3. 若 task.round == 0：時間到，觸發執行
//   4. 若 task.round > 0：圈數遞減
//
// 時間複雜度：O(當前槽位任務數)
func (tw *TimingWheel) tick() {
	tw.mu.Lock()

	// 1. 指針前進
	tw.currentSlot = (tw.currentSlot + 1) % SlotCount

	// 2. 獲取當前槽位
	slot := tw.slots[tw.currentSlot]

	// 3. 收集需要執行的任務
	var tasksToExecute []*Task
	var next *list.Element

	for e := slot.Front(); e != nil; e = next {
		next = e.Next()
		task := e.Value.(*Task)

		if task.Round == 0 {
			// 時間到，加入執行列表
			tasksToExecute = append(tasksToExecute, task)
			slot.Remove(e)
		} else {
			// 圈數遞減
			task.Round--
		}
	}

	tw.mu.Unlock()

	// 4. 觸發執行（在鎖外執行，避免阻塞）
	if len(tasksToExecute) > 0 && tw.onTick != nil {
		tw.onTick(tasksToExecute)
	}
}

// Stop 停止時間輪
func (tw *TimingWheel) Stop() {
	tw.ticker.Stop()
}

// Size 返回時間輪中的任務總數
func (tw *TimingWheel) Size() int {
	tw.mu.RLock()
	defer tw.mu.RUnlock()

	count := 0
	for i := 0; i < SlotCount; i++ {
		count += tw.slots[i].Len()
	}
	return count
}
