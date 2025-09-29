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