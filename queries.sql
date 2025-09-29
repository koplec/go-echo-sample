-- name: CreateUser :one
INSERT INTO users (email, age, name, bio, is_active, additional_data)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = ?;

-- name: ListUsers :many
SELECT * FROM users
WHERE is_active = true
ORDER BY created_at DESC
LIMIT ?;

-- name: UpdateUser :one
UPDATE users
SET email = ?, age = ?, name = ?, bio = ?, is_active = ?, additional_data = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = ?;

-- Job Queue Operations
-- name: CreateJob :one
INSERT INTO job_queue (job_type, payload, priority, max_retries, scheduled_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetNextPendingJob :one
SELECT * FROM job_queue
WHERE status = 'pending'
  AND scheduled_at <= CURRENT_TIMESTAMP
  AND retry_count < max_retries
ORDER BY priority DESC, scheduled_at ASC
LIMIT 1;

-- name: UpdateJobStatus :one
UPDATE job_queue
SET status = ?, started_at = ?, completed_at = ?, error_message = ?
WHERE id = ?
RETURNING *;

-- name: IncrementJobRetry :one
UPDATE job_queue
SET retry_count = retry_count + 1,
    status = 'pending',
    scheduled_at = datetime(CURRENT_TIMESTAMP, '+' || (retry_count + 1) * 5 || ' minutes'),
    error_message = ?
WHERE id = ?
RETURNING *;

-- name: GetJobByID :one
SELECT * FROM job_queue
WHERE id = ?;

-- name: ListJobs :many
SELECT * FROM job_queue
WHERE status = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: GetJobStats :one
SELECT
    COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending_count,
    COUNT(CASE WHEN status = 'processing' THEN 1 END) as processing_count,
    COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed_count,
    COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_count
FROM job_queue;