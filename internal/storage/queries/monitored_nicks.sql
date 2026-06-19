-- name: AddMonitoredNick :exec
INSERT INTO monitored_nicks (network_id, nickname, created_at)
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(network_id, nickname) DO NOTHING;

-- name: RemoveMonitoredNick :exec
DELETE FROM monitored_nicks WHERE network_id = ? AND nickname = ?;

-- name: GetMonitoredNicks :many
SELECT * FROM monitored_nicks WHERE network_id = ? ORDER BY nickname;
