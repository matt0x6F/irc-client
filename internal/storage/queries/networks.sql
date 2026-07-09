-- name: GetNetwork :one
SELECT * FROM networks WHERE id = ?;

-- name: GetNetworks :many
SELECT * FROM networks ORDER BY sort_order, id;

-- name: CreateNetwork :one
INSERT INTO networks (name, address, port, tls, nickname, username, realname, password, sasl_enabled, sasl_mechanism, sasl_username, sasl_password, sasl_external_cert, auto_connect, identify_as_bot, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateNetwork :exec
UPDATE networks
SET name = ?, address = ?, port = ?, tls = ?,
    nickname = ?, username = ?, realname = ?,
    password = ?, sasl_enabled = ?, sasl_mechanism = ?,
    sasl_username = ?, sasl_password = ?, sasl_external_cert = ?,
    auto_connect = ?, identify_as_bot = ?, updated_at = ?
WHERE id = ?;

-- name: UpdateNetworkAutoConnect :exec
UPDATE networks SET auto_connect = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: UpdateNetworkColor :exec
UPDATE networks SET color = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: UpdateNetworkIcon :exec
UPDATE networks SET icon_path = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: ClearNetworkIcon :exec
UPDATE networks SET icon_path = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: UpdateNetworkSortOrder :exec
UPDATE networks SET sort_order = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: DeleteNetwork :exec
DELETE FROM networks WHERE id = ?;
