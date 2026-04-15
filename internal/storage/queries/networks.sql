-- name: GetNetwork :one
SELECT * FROM networks WHERE id = ?;

-- name: GetNetworks :many
SELECT * FROM networks ORDER BY name;

-- name: CreateNetwork :one
INSERT INTO networks (name, address, port, tls, nickname, username, realname, password, sasl_enabled, sasl_mechanism, sasl_username, sasl_password, sasl_external_cert, auto_connect, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateNetwork :exec
UPDATE networks 
SET name = ?, address = ?, port = ?, tls = ?, 
    nickname = ?, username = ?, realname = ?, 
    password = ?, sasl_enabled = ?, sasl_mechanism = ?,
    sasl_username = ?, sasl_password = ?, sasl_external_cert = ?,
    auto_connect = ?, updated_at = ?
WHERE id = ?;

-- name: UpdateNetworkAutoConnect :exec
UPDATE networks SET auto_connect = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: DeleteNetwork :exec
DELETE FROM networks WHERE id = ?;
