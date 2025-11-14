package shortener

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/koopa0/system-design/03-url-shortener/pkg/base62"
	"github.com/koopa0/system-design/03-url-shortener/pkg/snowflake"
)

// Shorten 將長 URL 轉換為短網址
//
// 參數：
//   - ctx：上下文（用於超時控制）
//   - store：存儲接口
//   - idgen：Snowflake ID 生成器
//   - longURL：原始完整 URL
//   - customCode：自定義短碼（可選，傳空字符串則自動生成）
//   - expiresAt：過期時間（可選，傳 nil 則永不過期）
//
// 返回：
//   - URL 記錄
//   - 錯誤（ErrInvalidURL、ErrCodeExists 或存儲錯誤）
//
// 算法流程：
//  1. 驗證 URL 格式
//  2. 生成短碼：
//     - 如果提供 customCode，使用自定義碼（需驗證有效性）
//     - 否則，生成 Snowflake ID → Base62 編碼
//  3. 構建 URL 記錄
//  4. 保存到存儲層
//
// 系統設計考量：
//   - ID 生成：使用 Snowflake（分布式、趨勢遞增、適合資料庫索引）
//   - 短碼編碼：使用 Base62（URL 友好、比 Base64 更安全）
//   - 衝突處理：依賴存儲層的原子性（如 PostgreSQL UNIQUE 約束）
//   - 自定義短碼：允許用戶自定義（如品牌短鏈 bit.ly/google-io）
func Shorten(ctx context.Context, store Store, idgen *snowflake.Generator, longURL string, customCode string, expiresAt *time.Time) (*URL, error) {
	// 1. 驗證 URL 格式
	//
	// 系統設計考量：
	//   - 防止無效 URL（如 "javascript:alert(1)"）
	//   - 要求完整的 scheme（http:// 或 https://）
	if !isValidURL(longURL) {
		return nil, ErrInvalidURL
	}

	// 2. 生成短碼
	var shortCode string
	var id int64

	if customCode != "" {
		// 使用自定義短碼
		//
		// 驗證：
		//   - 長度限制：4-12 字符（平衡可讀性與容量）
		//   - 僅允許 Base62 字符（0-9, A-Z, a-z）
		//   - 防止注入攻擊（如 "../admin"）
		if len(customCode) < 4 || len(customCode) > 12 {
			return nil, ErrInvalidURL
		}
		if !base62.IsValid(customCode) {
			return nil, ErrInvalidURL
		}
		shortCode = customCode

		// 仍然生成 ID（用於資料庫主鍵）
		var err error
		id, err = idgen.Generate()
		if err != nil {
			return nil, err
		}
	} else {
		// 自動生成短碼
		//
		// 流程：
		//   1. 生成 Snowflake ID（64 位整數）
		//   2. 轉換為 Base62 字符串
		//
		// 短碼長度分析：
		//   - Snowflake ID 最大值：2^63 - 1
		//   - Base62 編碼後：約 11 位字符
		//   - 實際使用（從 2024 年開始）：約 7-8 位
		//
		// 容量計算：
		//   - 7 位 Base62：62^7 = 3.5 兆（3.5 trillion）
		//   - 足夠使用數十年
		var err error
		id, err = idgen.Generate()
		if err != nil {
			return nil, err
		}

		// Base62 編碼
		//
		// 為什麼用 Base62 而非 Base64？
		//   - Base64 包含 + 和 /（URL 中需要轉義）
		//   - Base62 僅用 0-9, A-Z, a-z（URL 友好）
		//   - 犧牲 3% 的壓縮率，換取更好的兼容性
		shortCode = base62.Encode(uint64(id))
	}

	// 3. 構建 URL 記錄
	now := time.Now()
	urlRecord := &URL{
		ID:        id,
		ShortCode: shortCode,
		LongURL:   longURL,
		Clicks:    0,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	// 4. 保存到存儲層
	//
	// 系統設計考量：
	//   - 衝突檢測：由存儲層保證（UNIQUE 約束）
	//   - 原子性：避免競態條件
	//   - 錯誤處理：如果短碼已存在，返回 ErrCodeExists
	if err := store.Save(ctx, urlRecord); err != nil {
		return nil, err
	}

	return urlRecord, nil
}

// isValidURL 驗證 URL 格式
//
// 驗證規則：
//   - 必須可解析（url.Parse）
//   - 必須有 scheme（http:// 或 https://）
//   - 必須有 host（域名或 IP）
//   - 不允許私有 IP 範圍（防止 SSRF）
//
// 安全考量：
//   - 拒絕 javascript:、data: 等危險 scheme
//   - 防止 SSRF 攻擊：拒絕 localhost、127.0.0.1、私有 IP
//   - 設計問題：是否允許內網 URL？取決於業務需求
func isValidURL(rawURL string) bool {
	// 解析 URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// 檢查 scheme（僅允許 http 和 https）
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}

	// 檢查 host（必須存在）
	if u.Host == "" {
		return false
	}

	// SSRF 防護：檢查是否為私有 IP 或 localhost
	//
	// 系統設計考量：
	//   - 為什麼拒絕私有 IP？
	//     → 防止攻擊者探測內網服務（如 http://192.168.1.1/admin）
	//     → 防止訪問雲服務元數據端點（如 http://169.254.169.254）
	//   - Trade-off：如果業務需要內網短鏈，需要配置白名單
	if isPrivateOrLocalhost(u.Hostname()) {
		return false
	}

	return true
}

// isPrivateOrLocalhost 檢查主機名是否為私有 IP 或 localhost
//
// 防護範圍：
//   - localhost、127.0.0.0/8（本地回環）
//   - 10.0.0.0/8（私有網段 A）
//   - 172.16.0.0/12（私有網段 B）
//   - 192.168.0.0/16（私有網段 C）
//   - 169.254.0.0/16（鏈路本地地址，AWS 元數據）
//
// 系統設計問題：
//   - 為什麼要防止 169.254.169.254？
//     → AWS/GCP 的元數據服務地址
//     → 攻擊者可以通過 SSRF 獲取雲服務憑證
func isPrivateOrLocalhost(host string) bool {
	// 檢查 localhost
	if host == "localhost" || strings.HasPrefix(host, "127.") {
		return true
	}

	// 嘗試解析為 IP
	ip := net.ParseIP(host)
	if ip == nil {
		// 如果不是 IP，嘗試解析域名（簡化處理：允許域名通過）
		// 生產環境應該：
		//   1. 解析域名為 IP
		//   2. 檢查所有 IP 是否為私有
		//   3. 設置解析超時（避免 DNS rebinding）
		return false
	}

	// 檢查私有 IP 範圍
	privateRanges := []string{
		"10.0.0.0/8",     // 私有網段 A
		"172.16.0.0/12",  // 私有網段 B
		"192.168.0.0/16", // 私有網段 C
		"169.254.0.0/16", // 鏈路本地地址（AWS 元數據）
		"127.0.0.0/8",    // 本地回環
	}

	for _, cidr := range privateRanges {
		_, ipNet, _ := net.ParseCIDR(cidr)
		if ipNet.Contains(ip) {
			return true
		}
	}

	return false
}
