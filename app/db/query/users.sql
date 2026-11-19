-- name: CreateUser :one
INSERT INTO users (full_name, email, phone, password_hash, role)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (email) DO NOTHING
RETURNING id;

-- name: GetUserByEmail :one
SELECT id, full_name, email, phone, password_hash, role, status
FROM users
WHERE email = ?;

-- name: GetUserByID :one
SELECT id, full_name, email, phone, password_hash, role, status
FROM users
WHERE id = ?;
