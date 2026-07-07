-- name: CreateActivityItem :one
INSERT INTO activity_items (network_id, source_type, target, actor, preview, msgid, keyword, seen, timestamp, trusted, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListActivityItems :many
SELECT * FROM activity_items ORDER BY timestamp DESC, id DESC LIMIT ?;

-- name: MarkActivityItemSeen :exec
UPDATE activity_items SET seen = 1 WHERE id = ?;

-- name: MarkAllActivityItemsSeen :exec
UPDATE activity_items SET seen = 1 WHERE seen = 0;

-- name: DeleteActivityItem :exec
DELETE FROM activity_items WHERE id = ?;

-- name: DeleteSeenActivityItems :exec
DELETE FROM activity_items WHERE seen = 1;

-- name: DeleteAllActivityItems :exec
DELETE FROM activity_items;

-- name: ListInviteActivity :many
SELECT * FROM activity_items
WHERE source_type = 'invite' AND network_id = ?
  AND (expires_at IS NULL OR expires_at > ?)
ORDER BY timestamp DESC, id DESC;

-- name: DeleteInviteActivity :exec
DELETE FROM activity_items
WHERE source_type = 'invite' AND network_id = ? AND LOWER(actor) = LOWER(sqlc.arg(actor)) AND LOWER(target) = LOWER(sqlc.arg(target));

-- name: DeleteInviteActivityFromSender :exec
DELETE FROM activity_items
WHERE source_type = 'invite' AND network_id = ? AND LOWER(actor) = LOWER(sqlc.arg(actor));

-- name: DeleteActivityFromSender :exec
DELETE FROM activity_items
WHERE network_id = ? AND LOWER(actor) = LOWER(sqlc.arg(actor));

-- name: NetworksWithExpiredInvites :many
SELECT DISTINCT network_id FROM activity_items
WHERE source_type = 'invite' AND expires_at IS NOT NULL AND expires_at <= ?;

-- name: DeleteExpiredInviteActivity :exec
DELETE FROM activity_items
WHERE source_type = 'invite' AND expires_at IS NOT NULL AND expires_at <= ?;
