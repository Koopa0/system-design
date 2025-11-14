package shortener

import (
	"context"
)

// Resolve 將短碼解析為長 URL
//
// 參數：
//   - ctx：上下文（用於超時控制）
//   - store：存儲接口
//   - shortCode：短碼（如 "8M0kX"）
//
// 返回：
//   - 長 URL（如 "https://example.com/very/long/url"）
//   - 錯誤（ErrNotFound、ErrExpired 或存儲錯誤）
//
// 算法流程：
//  1. 從存儲層加載 URL 記錄
//  2. 檢查是否過期
//  3. 異步增加點擊計數（不阻塞重定向）
//  4. 返回長 URL
//
// 系統設計考量：
//  - 性能優化：這是最高頻的操作（每次點擊短鏈都會調用）
//  - 快取策略：存儲層應實現快取（如 Redis）
//  - 點擊統計：異步更新，不影響重定向速度
//  - 過期處理：返回 410 Gone（而非 404 Not Found）
//
// 性能分析：
//  - QPS 目標：10,000+（短網址服務的核心指標）
//  - 延遲目標：< 10ms（p99）
//  - 優化手段：
//    1. Redis 快取（熱點數據）
//    2. 異步統計（點擊計數）
//    3. 連接池（資料庫）
func Resolve(ctx context.Context, store Store, shortCode string) (string, error) {
	// 1. 從存儲層加載 URL 記錄
	//
	// 系統設計考量：
	//   - 這裡會先查 Redis 快取（由存儲層實現）
	//   - Cache Miss 時查資料庫
	//   - 使用 Cache-Aside 模式（詳見 storage 實現）
	urlRecord, err := store.Load(ctx, shortCode)
	if err != nil {
		return "", err
	}

	// 2. 檢查過期
	//
	// 為什麼在這裡檢查而不是存儲層？
	//   - 業務邏輯應該在業務層
	//   - 存儲層只負責數據的 CRUD
	//   - 過期檢查是業務規則，不是存儲規則
	if urlRecord.IsExpired() {
		return "", ErrExpired
	}

	// 3. 增加點擊計數
	//
	// 系統設計考量：
	//   - 異步操作：使用 goroutine，不阻塞重定向
	//   - 允許失敗：統計不準確可接受，但重定向必須成功
	//   - 性能優先：犧牲一致性，換取低延遲
	//
	// 更好的實現（生產環境）：
	//   - 使用消息隊列（NATS/NSQ）
	//   - 批量更新（每秒聚合一次）
	//   - 分離統計服務（避免影響核心流程）
	//
	// 這裡簡化處理：啟動 goroutine 異步更新
	go func() {
		// 使用新的 context（避免父 context 取消影響）
		//
		// 為什麼不用 ctx？
		//   - ctx 會在請求結束時取消
		//   - 異步操作需要獨立的生命週期
		//
		// 生產環境應該：
		//   - context.WithTimeout(context.Background(), 5*time.Second)
		//   - 記錄錯誤日誌
		//   - 監控失敗率
		_ = store.IncrementClicks(context.Background(), shortCode)
		// 忽略錯誤（統計失敗不影響重定向）
	}()

	// 4. 返回長 URL
	return urlRecord.LongURL, nil
}
