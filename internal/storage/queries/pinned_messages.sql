-- name: PinMessage :exec
INSERT INTO pinned_messages (message_id, network_id, channel_id, pinned_by, pinned_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(message_id) DO NOTHING;

-- name: UnpinMessage :exec
DELETE FROM pinned_messages WHERE message_id = ?;

-- name: GetPinnedMessagesWithChannel :many
SELECT m.id, m.network_id, m.channel_id, m.user, m.message, m.message_type, m.timestamp, m.raw_line, m.pm_target,
       p.pinned_by, p.pinned_at
FROM pinned_messages p
JOIN messages m ON m.id = p.message_id
WHERE p.network_id = ? AND p.channel_id = ?
ORDER BY p.pinned_at DESC;

-- name: GetPinnedMessagesWithoutChannel :many
SELECT m.id, m.network_id, m.channel_id, m.user, m.message, m.message_type, m.timestamp, m.raw_line, m.pm_target,
       p.pinned_by, p.pinned_at
FROM pinned_messages p
JOIN messages m ON m.id = p.message_id
WHERE p.network_id = ? AND p.channel_id IS NULL
ORDER BY p.pinned_at DESC;

-- name: GetMessagesBeforeWithChannel :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id = ? AND id <= ?
ORDER BY id DESC
LIMIT ?;

-- name: GetMessagesAfterWithChannel :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id = ? AND id > ?
ORDER BY id ASC
LIMIT ?;

-- name: GetMessagesBeforeWithoutChannel :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id IS NULL AND pm_target IS NULL AND id <= ?
ORDER BY id DESC
LIMIT ?;

-- name: GetMessagesAfterWithoutChannel :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id IS NULL AND pm_target IS NULL AND id > ?
ORDER BY id ASC
LIMIT ?;

-- Timestamp-keyed "before" pagination. Unlike the id-keyed variants above, these
-- correctly include CHATHISTORY-backfilled rows, which are inserted now (high id)
-- but carry old server-time timestamps. The id tiebreaker keeps ordering stable
-- for rows sharing a timestamp.

-- name: GetMessagesBeforeTimeWithChannel :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id = ? AND timestamp < ?
ORDER BY timestamp DESC, id DESC
LIMIT ?;

-- name: GetMessagesBeforeTimeWithoutChannel :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id IS NULL AND pm_target IS NULL AND timestamp < ?
ORDER BY timestamp DESC, id DESC
LIMIT ?;

-- name: GetMessagesBeforeTimePM :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id IS NULL
  AND message_type IN ('privmsg', 'action', 'notice')
  AND LOWER(pm_target) = ? AND timestamp < ?
ORDER BY timestamp DESC, id DESC
LIMIT ?;
