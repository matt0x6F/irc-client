-- name: AddIgnoredSender :exec
INSERT INTO activity_ignored_senders (network_id, nick, created_at)
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(network_id, nick) DO NOTHING;

-- name: RemoveIgnoredSender :exec
DELETE FROM activity_ignored_senders
WHERE network_id = ? AND LOWER(nick) = LOWER(sqlc.arg(nick));

-- name: ListIgnoredSendersByNetwork :many
SELECT nick FROM activity_ignored_senders WHERE network_id = ? ORDER BY nick;

-- name: ListAllIgnoredSenders :many
SELECT s.network_id, n.name AS network_name, s.nick
FROM activity_ignored_senders s
JOIN networks n ON n.id = s.network_id
ORDER BY n.name, s.nick;

-- name: CountIgnoredSender :one
SELECT COUNT(*) FROM activity_ignored_senders
WHERE network_id = ? AND LOWER(nick) = LOWER(sqlc.arg(nick));
