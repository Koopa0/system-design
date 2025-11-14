-- name: GetCounter :one
-- 獲取單個計數器的當前值
SELECT * FROM counters
WHERE name = $1 LIMIT 1;

-- name: GetCounters :many
-- 批量獲取多個計數器
SELECT * FROM counters
WHERE name = ANY($1::text[]);

-- name: CreateCounter :one
-- 創建新的計數器
INSERT INTO counters (
    name, counter_type, metadata
) VALUES (
    $1, $2::counter_type, $3
) ON CONFLICT (name) DO NOTHING
RETURNING *;

-- name: IncrementCounter :one
-- 原子性增加計數器值
UPDATE counters 
SET current_value = current_value + $2,
    updated_at = NOW()
WHERE name = $1
RETURNING current_value;

-- name: DecrementCounter :one
-- 原子性減少計數器值
UPDATE counters 
SET current_value = GREATEST(0, current_value - $2),
    updated_at = NOW()
WHERE name = $1
RETURNING current_value;

-- name: SetCounter :exec
-- 直接設置計數器值（用於從 Redis 同步）
UPDATE counters 
SET current_value = $2,
    updated_at = NOW()
WHERE name = $1;

-- name: ResetCounter :exec
-- 重置計數器為 0
UPDATE counters 
SET current_value = 0,
    updated_at = NOW()
WHERE name = $1;

-- name: ListCounters :many
-- 列出所有計數器
SELECT * FROM counters
ORDER BY name
LIMIT $1 OFFSET $2;

-- name: ArchiveCounterHistory :one
-- 歸檔計數器歷史記錄
INSERT INTO counter_history (
    counter_name, date, final_value, unique_users, metadata
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetCounterHistory :many
-- 查詢計數器歷史
SELECT * FROM counter_history
WHERE counter_name = $1 
  AND date >= $2 
  AND date <= $3
ORDER BY date DESC;

-- name: DeleteOldHistory :exec
-- 刪除超過 7 天的歷史記錄
DELETE FROM counter_history
WHERE date < CURRENT_DATE - INTERVAL '7 days';

-- name: EnqueueWrite :one
-- 將寫入操作加入佇列（降級模式使用）
INSERT INTO write_queue (
    counter_name, operation, value, user_id, metadata
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: DequeueWrites :many
-- 獲取未處理的寫入操作
SELECT * FROM write_queue
WHERE processed = FALSE
ORDER BY created_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkWriteProcessed :exec
-- 標記寫入操作為已處理
UPDATE write_queue
SET processed = TRUE
WHERE id = $1;

-- name: CleanOldQueue :exec
-- 清理已處理的舊佇列項目
DELETE FROM write_queue
WHERE processed = TRUE 
  AND created_at < NOW() - INTERVAL '1 hour';