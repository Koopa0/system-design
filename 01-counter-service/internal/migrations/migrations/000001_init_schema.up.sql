-- 建立計數器類型枚舉
CREATE TYPE counter_type AS ENUM ('normal', 'unique', 'cumulative');

-- 計數器主表
CREATE TABLE IF NOT EXISTS counters (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    current_value BIGINT DEFAULT 0,
    counter_type counter_type DEFAULT 'normal',
    metadata JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- 建立索引以加速查詢
CREATE INDEX idx_counters_name ON counters(name);
CREATE INDEX idx_counters_type ON counters(counter_type);

-- 計數器歷史記錄表
CREATE TABLE IF NOT EXISTS counter_history (
    id SERIAL PRIMARY KEY,
    counter_name VARCHAR(100) NOT NULL,
    date DATE NOT NULL,
    final_value BIGINT NOT NULL,
    unique_users JSONB,
    metadata JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(counter_name, date)
);

-- 建立複合索引以加速查詢
CREATE INDEX idx_counter_history_name_date ON counter_history(counter_name, date);
CREATE INDEX idx_counter_history_date ON counter_history(date);

-- 寫入佇列表（用於降級模式）
CREATE TABLE IF NOT EXISTS write_queue (
    id SERIAL PRIMARY KEY,
    counter_name VARCHAR(100) NOT NULL,
    operation VARCHAR(20) NOT NULL,
    value BIGINT NOT NULL,
    user_id VARCHAR(100),
    metadata JSONB,
    processed BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW()
);

-- 建立索引
CREATE INDEX idx_write_queue_processed ON write_queue(processed);
CREATE INDEX idx_write_queue_created_at ON write_queue(created_at);

-- 更新時間觸發器函式
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- 應用觸發器到 counters 表
CREATE TRIGGER update_counters_updated_at
    BEFORE UPDATE ON counters
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- 插入預設計數器
INSERT INTO counters (name, current_value, counter_type) VALUES 
    ('online_players', 0, 'normal'),
    ('daily_active_users', 0, 'unique'),
    ('total_games_played', 0, 'cumulative')
ON CONFLICT (name) DO NOTHING;