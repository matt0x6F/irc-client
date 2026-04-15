-- name: GetServers :many
SELECT * FROM servers 
WHERE network_id = ? 
ORDER BY "order" ASC, id ASC;

-- name: CreateServer :one
INSERT INTO servers (network_id, address, port, tls, "order", created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateServer :exec
UPDATE servers 
SET address = ?, port = ?, tls = ?, "order" = ?
WHERE id = ?;

-- name: DeleteServer :exec
DELETE FROM servers WHERE id = ?;

-- name: DeleteAllServers :exec
DELETE FROM servers WHERE network_id = ?;
