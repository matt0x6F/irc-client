-- name: GetMessagesWithChannel :many
SELECT * FROM messages 
WHERE network_id = ? AND channel_id = ? 
ORDER BY timestamp DESC 
LIMIT ?;

-- name: GetMessagesWithoutChannel :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id IS NULL AND pm_target IS NULL
ORDER BY timestamp DESC
LIMIT ?;

-- name: CreateMessage :one
INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line, pm_target, msgid, reply_msgid, channel_context)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMessageByMsgID :one
SELECT * FROM messages
WHERE network_id = ? AND msgid = ?
LIMIT 1;

-- name: GetPrivateMessages :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id IS NULL AND message_type IN ('privmsg', 'action', 'notice', 'marker')
AND LOWER(pm_target) = ?
ORDER BY timestamp DESC
LIMIT ?;
