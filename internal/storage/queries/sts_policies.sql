-- name: UpsertSTSPolicy :exec
INSERT INTO sts_policies (hostname, port, expires_at, created_at, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(hostname) DO UPDATE SET
    port = excluded.port,
    expires_at = excluded.expires_at,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetSTSPolicy :one
SELECT * FROM sts_policies WHERE hostname = ?;

-- name: GetSTSPolicies :many
SELECT * FROM sts_policies ORDER BY hostname;

-- name: DeleteSTSPolicy :exec
DELETE FROM sts_policies WHERE hostname = ?;
