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
INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line, pm_target, conversation_id, msgid, reply_msgid, channel_context)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMessageByMsgID :one
SELECT * FROM messages
WHERE network_id = ? AND msgid = ?
LIMIT 1;

-- name: GetMessageIDByMsgID :one
SELECT id FROM messages WHERE network_id = ? AND msgid = ? LIMIT 1;

-- name: GetPrivateMessages :many
SELECT * FROM messages
WHERE network_id = ? AND channel_id IS NULL AND message_type IN ('privmsg', 'action', 'notice', 'marker')
AND LOWER(pm_target) = ?
AND conversation_id IS NULL
ORDER BY timestamp DESC
LIMIT ?;

-- name: GetConversationMessages :many
SELECT * FROM messages WHERE network_id = ? AND channel_id IS NULL AND conversation_id = ?
ORDER BY timestamp DESC, id DESC LIMIT ?;

-- name: GetConversationMessagesBeforeTime :many
SELECT * FROM messages WHERE network_id = ? AND channel_id IS NULL AND conversation_id = ? AND timestamp < ?
ORDER BY timestamp DESC, id DESC LIMIT ?;
