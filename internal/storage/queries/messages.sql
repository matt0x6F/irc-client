-- name: GetMessagesWithChannel :many
SELECT * FROM messages 
WHERE network_id = ? AND channel_id = ? 
ORDER BY timestamp DESC 
LIMIT ?;

-- name: GetMessagesWithoutChannel :many
SELECT * FROM messages 
WHERE network_id = ? AND channel_id IS NULL 
ORDER BY timestamp DESC 
LIMIT ?;

-- name: CreateMessage :one
INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPrivateMessages :many
SELECT * FROM messages 
WHERE network_id = ? AND channel_id IS NULL AND message_type IN ('privmsg', 'action')
AND (
  LOWER(user) = ? OR 
  (LOWER(user) = ? AND LOWER(raw_line) LIKE ?)
)
ORDER BY timestamp DESC 
LIMIT ?;
