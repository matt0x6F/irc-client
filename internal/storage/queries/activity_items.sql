-- name: CreateActivityItem :one
INSERT INTO activity_items (network_id, source_type, target, actor, preview, msgid, keyword, seen, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
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
