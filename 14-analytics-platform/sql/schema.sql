-- ClickHouse Schema for Analytics Platform
-- 分析平台數據庫表結構

-- ============================================================================
-- 事實表（Fact Table）：存儲業務事件
-- ============================================================================

-- 訂單事實表
CREATE TABLE IF NOT EXISTS fact_orders (
    order_id UInt64,
    user_id UInt32,
    product_id UInt32,
    amount Decimal(10, 2),
    province String,
    category String,
    payment_method Enum8('credit_card' = 1, 'debit_card' = 2, 'cash' = 3, 'ewallet' = 4),
    order_date Date,
    order_hour UInt8,
    created_at DateTime,
    updated_at DateTime DEFAULT now()
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(order_date)  -- 按月分區
ORDER BY (order_date, province, category)  -- 排序鍵（影響查詢性能）
SETTINGS index_granularity = 8192;  -- 索引粒度

-- 評論事實表
CREATE TABLE IF NOT EXISTS fact_comments (
    comment_id UInt64,
    user_id UInt32,
    product_id UInt32,
    rating UInt8,  -- 1-5 星
    comment_text String,
    created_at DateTime
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(created_at)
ORDER BY (created_at, product_id);

-- 點擊流事實表
CREATE TABLE IF NOT EXISTS fact_clickstream (
    event_id UInt64,
    user_id UInt32,
    session_id String,
    page_url String,
    event_type Enum8('page_view' = 1, 'click' = 2, 'add_to_cart' = 3, 'purchase' = 4),
    timestamp DateTime
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, user_id);

-- ============================================================================
-- 維度表（Dimension Table）：存儲描述性信息
-- ============================================================================

-- 用戶維度表
CREATE TABLE IF NOT EXISTS dim_users (
    user_id UInt32,
    name String,
    email String,
    age UInt8,
    gender Enum8('M' = 1, 'F' = 2, 'Other' = 3),
    province String,
    city String,
    registration_date Date,
    vip_level UInt8  -- 0-5
) ENGINE = MergeTree()
ORDER BY user_id;

-- 商品維度表
CREATE TABLE IF NOT EXISTS dim_products (
    product_id UInt32,
    product_name String,
    category String,
    subcategory String,
    brand String,
    price Decimal(10, 2),
    cost Decimal(10, 2),
    created_at DateTime
) ENGINE = MergeTree()
ORDER BY product_id;

-- 日期維度表（用於日曆分析）
CREATE TABLE IF NOT EXISTS dim_date (
    date Date,
    year UInt16,
    quarter UInt8,
    month UInt8,
    day UInt8,
    day_of_week UInt8,  -- 1-7 (Monday-Sunday)
    week_of_year UInt8,
    is_weekend UInt8,   -- 0 or 1
    is_holiday UInt8,   -- 0 or 1
    holiday_name String
) ENGINE = MergeTree()
ORDER BY date;

-- ============================================================================
-- 物化視圖（Materialized Views）：預聚合常用查詢
-- ============================================================================

-- 每日銷售額（按類目）
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_daily_sales
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(order_date)
ORDER BY (category, order_date)
AS
SELECT
    category,
    order_date,
    sum(amount) as daily_sales,
    count() as order_count,
    avg(amount) as avg_order_value,
    uniq(user_id) as unique_users
FROM fact_orders
GROUP BY category, order_date;

-- 每小時訂單量（按省份）
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_hourly_orders_by_province
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMMDD(order_date)
ORDER BY (province, order_date, order_hour)
AS
SELECT
    province,
    order_date,
    order_hour,
    count() as order_count,
    sum(amount) as total_amount
FROM fact_orders
GROUP BY province, order_date, order_hour;

-- 用戶活躍度（每日）
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_daily_active_users
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(order_date)
ORDER BY order_date
AS
SELECT
    order_date,
    uniqState(user_id) as active_users  -- 使用 State 聚合函數
FROM fact_orders
GROUP BY order_date;

-- 商品銷售排行
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_product_sales_rank
ENGINE = SummingMergeTree()
ORDER BY (order_date, product_id)
AS
SELECT
    order_date,
    product_id,
    count() as sales_count,
    sum(amount) as total_revenue
FROM fact_orders
GROUP BY order_date, product_id;

-- ============================================================================
-- 實時數據表（從 Kafka 攝取）
-- ============================================================================

-- 實時訂單表（Kafka 引擎）
CREATE TABLE IF NOT EXISTS realtime_orders (
    order_id UInt64,
    user_id UInt32,
    product_id UInt32,
    amount Decimal(10, 2),
    province String,
    category String,
    created_at DateTime
) ENGINE = Kafka
SETTINGS
    kafka_broker_list = 'kafka:9092',
    kafka_topic_list = 'orders',
    kafka_group_name = 'clickhouse_consumer',
    kafka_format = 'JSONEachRow',
    kafka_num_consumers = 4;

-- 物化視圖：將 Kafka 數據寫入 fact_orders
CREATE MATERIALIZED VIEW IF NOT EXISTS realtime_orders_consumer TO fact_orders
AS
SELECT
    order_id,
    user_id,
    product_id,
    amount,
    province,
    category,
    toDate(created_at) as order_date,
    toHour(created_at) as order_hour,
    created_at,
    now() as updated_at
FROM realtime_orders;

-- ============================================================================
-- 索引（加速查詢）
-- ============================================================================

-- 跳數索引（Skipping Index）：加速範圍查詢
ALTER TABLE fact_orders ADD INDEX idx_amount amount TYPE minmax GRANULARITY 4;

-- 布隆過濾器索引：加速等值查詢
ALTER TABLE fact_orders ADD INDEX idx_user_id user_id TYPE bloom_filter GRANULARITY 1;

-- ============================================================================
-- 查詢示例
-- ============================================================================

-- 示例 1：查詢今日各類目銷售額（使用物化視圖）
-- SELECT
--     category,
--     sum(daily_sales) as total_sales,
--     sum(order_count) as total_orders
-- FROM mv_daily_sales
-- WHERE order_date = today()
-- GROUP BY category
-- ORDER BY total_sales DESC;

-- 示例 2：查詢過去 7 天的銷售趨勢
-- SELECT
--     order_date,
--     sum(daily_sales) as total_sales
-- FROM mv_daily_sales
-- WHERE order_date >= today() - INTERVAL 7 DAY
-- GROUP BY order_date
-- ORDER BY order_date;

-- 示例 3：查詢各省份各時段的訂單量（使用物化視圖）
-- SELECT
--     province,
--     order_hour,
--     sum(order_count) as total_orders
-- FROM mv_hourly_orders_by_province
-- WHERE order_date = today()
-- GROUP BY province, order_hour
-- ORDER BY province, order_hour;

-- 示例 4：查詢用戶購買行為（關聯維度表）
-- SELECT
--     u.province,
--     u.age,
--     o.category,
--     count() as purchase_count,
--     sum(o.amount) as total_spent
-- FROM fact_orders o
-- JOIN dim_users u ON o.user_id = u.user_id
-- WHERE o.order_date >= today() - INTERVAL 30 DAY
-- GROUP BY u.province, u.age, o.category
-- ORDER BY total_spent DESC
-- LIMIT 100;

-- 示例 5：實時查詢（過去 5 分鐘的訂單）
-- SELECT
--     toStartOfMinute(created_at) as minute,
--     count() as order_count,
--     sum(amount) as total_amount
-- FROM fact_orders
-- WHERE created_at >= now() - INTERVAL 5 MINUTE
-- GROUP BY minute
-- ORDER BY minute;
