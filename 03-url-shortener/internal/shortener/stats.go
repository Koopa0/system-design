package shortener

import (
	"context"
)

// Stats 獲取短網址的統計信息
//
// 參數：
//   - ctx：上下文（用於超時控制）
//   - store：存儲接口
//   - shortCode：短碼（如 "8M0kX"）
//
// 返回：
//   - URL 記錄（包含點擊次數）
//   - 錯誤（ErrNotFound 或存儲錯誤）
//
// 系統設計考量：
//   - 這是低頻操作（相比重定向）
//   - 允許稍高的延遲（100ms 可接受）
//   - 不檢查過期（即使過期也返回統計）
//
// 使用場景：
//   - 用戶查看短鏈統計（點擊次數、創建時間）
//   - 管理後台（分析熱門鏈接）
//   - API 調用（第三方集成）
//
// 擴展功能（本示例未實現）：
//   - 點擊明細（時間、來源 IP、User-Agent）
//   - 地理分布（根據 IP 定位）
//   - 設備統計（PC/Mobile）
//   - 來源分析（Referer）
func Stats(ctx context.Context, store Store, shortCode string) (*URL, error) {
	// 直接從存儲層加載
	//
	// 注意：
	//   - 不檢查過期（即使過期也返回統計）
	//   - 返回完整的 URL 記錄（包含所有字段）
	urlRecord, err := store.Load(ctx, shortCode)
	if err != nil {
		return nil, err
	}

	return urlRecord, nil
}
