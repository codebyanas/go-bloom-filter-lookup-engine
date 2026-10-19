-- name: CheckUsernameExists :one
SELECT EXISTS(
    SELECT 1 FROM users WHERE username = $1
) AS exists;

-- name: CreateUser :one
INSERT INTO users (
    username, email
) VALUES (
    $1, $2
)
RETURNING id, username, email, created_at;