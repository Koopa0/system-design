-- 刪除觸發器
DROP TRIGGER IF EXISTS update_counters_updated_at ON counters;

-- 刪除函式
DROP FUNCTION IF EXISTS update_updated_at_column();

-- 刪除表
DROP TABLE IF EXISTS write_queue;
DROP TABLE IF EXISTS counter_history;
DROP TABLE IF EXISTS counters;

-- 刪除類型
DROP TYPE IF EXISTS counter_type;