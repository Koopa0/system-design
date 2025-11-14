-- URL Shortener 資料庫初始化腳本
--
-- 系統設計考量：
--   1. 表結構設計：支持高效查詢和擴展
--   2. 索引策略：優化讀取性能
--   3. 約束：保證數據一致性
--   4. 擴展性：為未來分片做準備

-- 創建 urls 表
CREATE TABLE IF NOT EXISTS urls (
    -- 主鍵：Snowflake ID（64-bit 整數）
    -- 系統設計：
    --   - 趨勢遞增：有利於 B-Tree 索引性能
    --   - 全局唯一：支持分布式生成
    --   - 包含時間戳：便於時間範圍查詢
    id BIGINT PRIMARY KEY,

    -- 短碼：用戶訪問的路徑（如 "8M0kX"）
    -- 系統設計：
    --   - UNIQUE 約束：防止衝突（原子性保證）
    --   - 索引：優化查詢（最高頻操作）
    --   - VARCHAR(20)：足夠長（Base62 編碼最多 11 字符）
    short_code VARCHAR(20) UNIQUE NOT NULL,

    -- 原始 URL
    -- 系統設計：
    --   - TEXT 類型：支持任意長度 URL
    --   - 不建索引：寫入時無需查詢
    long_url TEXT NOT NULL,

    -- 點擊統計
    -- 系統設計：
    --   - BIGINT：支持大量點擊（2^63-1）
    --   - DEFAULT 0：新記錄初始為 0
    --   - 更新方式：UPDATE ... SET clicks = clicks + 1（原子操作）
    clicks BIGINT DEFAULT 0,

    -- 創建時間
    -- 系統設計：
    --   - TIMESTAMP：精確到秒即可
    --   - NOT NULL：必須記錄
    --   - 索引：支持時間範圍查詢（如統計每天新增）
    created_at TIMESTAMP NOT NULL,

    -- 過期時間（可選）
    -- 系統設計：
    --   - NULL 表示永不過期
    --   - 惰性刪除：訪問時檢查
    --   - 定期清理：批量刪除過期記錄
    expires_at TIMESTAMP
);

-- 索引設計
--
-- 系統設計考量：
--   1. 查詢模式分析：
--      - 最高頻：WHERE short_code = ?（重定向）
--      - 中頻：WHERE created_at >= ? AND created_at < ?（統計）
--      - 低頻：WHERE id = ?（主鍵查詢）
--
--   2. 索引選擇：
--      - short_code：UNIQUE INDEX（查詢 + 唯一性約束）
--      - created_at：B-Tree INDEX（範圍查詢）
--      - id：PRIMARY KEY（自動創建聚簇索引）
--
--   3. 索引權衡：
--      ✅ 優點：加速查詢（從 O(n) 到 O(log n)）
--      ❌ 缺點：增加寫入開銷、佔用存儲空間
--      決策：讀多寫少，索引利大於弊

-- short_code 唯一索引（已通過 UNIQUE 約束自動創建）
-- CREATE UNIQUE INDEX idx_short_code ON urls(short_code);

-- created_at 索引（支持時間範圍查詢）
CREATE INDEX idx_created_at ON urls(created_at);

-- expires_at 索引（優化定期清理任務）
-- 設計問題：是否需要？
--   - 場景：定期刪除過期記錄（WHERE expires_at < NOW()）
--   - 權衡：如果過期數據比例低，可能不值得建索引
--   - 決策：先不建，根據實際使用情況調整
-- CREATE INDEX idx_expires_at ON urls(expires_at) WHERE expires_at IS NOT NULL;

-- 分片準備（未來擴展）
--
-- 系統設計考量：
--   當單表數據量達到億級時，需要分片（Sharding）
--
--   分片策略選項：
--     1. 按 short_code 哈希分片
--        - 優點：查詢均勻分布
--        - 缺點：無法做範圍查詢
--
--     2. 按 id 範圍分片
--        - 優點：時間局部性好
--        - 缺點：新數據集中在某個分片（熱點）
--
--     3. 一致性哈希
--        - 優點：易於擴容
--        - 缺點：實現複雜
--
--   當前：單表設計
--   未來：選擇方案 1（按 short_code 哈希）

-- 統計視圖（可選）
--
-- 用途：分析熱門鏈接、使用趨勢
--
-- CREATE VIEW top_urls AS
-- SELECT short_code, long_url, clicks, created_at
-- FROM urls
-- WHERE expires_at IS NULL OR expires_at > NOW()
-- ORDER BY clicks DESC
-- LIMIT 100;

-- 權限設置（生產環境）
--
-- 系統設計考量：
--   - 最小權限原則
--   - 應用只需 SELECT, INSERT, UPDATE
--   - 不需要 DELETE, DROP（防止誤操作）
--
-- 示例（需要根據實際用戶名調整）：
-- GRANT SELECT, INSERT, UPDATE ON urls TO app_user;

COMMENT ON TABLE urls IS 'URL 短網址記錄表';
COMMENT ON COLUMN urls.id IS 'Snowflake ID（分布式唯一）';
COMMENT ON COLUMN urls.short_code IS '短碼（Base62 編碼）';
COMMENT ON COLUMN urls.long_url IS '原始完整 URL';
COMMENT ON COLUMN urls.clicks IS '點擊統計（允許最終一致性）';
COMMENT ON COLUMN urls.created_at IS '創建時間';
COMMENT ON COLUMN urls.expires_at IS '過期時間（NULL 表示永不過期）';
