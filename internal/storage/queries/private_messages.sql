-- name: GetPrivateMessageConversationsOpen :many
SELECT COALESCE(
    (SELECT m.user FROM messages m
     WHERE m.network_id = pmc.network_id
       AND m.channel_id IS NULL
       AND LOWER(m.user) = pmc.target_user
       AND m.message_type IN ('privmsg', 'action', 'notice')
     ORDER BY m.timestamp DESC
     LIMIT 1),
    pmc.target_user
) as user
 FROM private_message_conversations pmc
 WHERE pmc.network_id = ? AND pmc.is_open = 1
 ORDER BY pmc.updated_at DESC, pmc.created_at DESC;

-- name: GetPrivateMessageConversationsAll :many
SELECT MAX(user) as user
 FROM messages
 WHERE network_id = ? AND channel_id IS NULL AND user != '*' AND LOWER(user) != ? AND message_type IN ('privmsg', 'action')
 GROUP BY LOWER(user)
 ORDER BY MAX(timestamp) DESC;

-- name: GetPMConversation :one
SELECT * FROM private_message_conversations WHERE network_id = ? AND LOWER(target_user) = sqlc.arg(target_user) ORDER BY updated_at DESC, id DESC LIMIT 1;

-- name: CreatePMConversation :one
INSERT INTO private_message_conversations (network_id, target_user, is_open, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetOpenPMConversations :many
SELECT * FROM private_message_conversations
WHERE network_id = ? AND is_open = 1
ORDER BY updated_at DESC, created_at DESC;

-- name: UpdatePMConversationIsOpen :exec
UPDATE private_message_conversations SET is_open = ?, updated_at = CURRENT_TIMESTAMP WHERE network_id = ? AND target_user = ?;

-- name: GetLastOpenChannel :one
SELECT network_id, name, updated_at
FROM channels
WHERE is_open = 1
ORDER BY COALESCE(updated_at, created_at) DESC, created_at DESC
LIMIT 1;

-- name: GetLastOpenPM :one
SELECT id, network_id, target_user, updated_at
FROM private_message_conversations
WHERE is_open = 1
ORDER BY COALESCE(updated_at, created_at) DESC, created_at DESC
LIMIT 1;

-- name: GetPMConversationByID :one
SELECT * FROM private_message_conversations WHERE network_id = ? AND id = ?;

-- name: UpdatePMConversationTarget :exec
UPDATE private_message_conversations SET target_user = ?, updated_at = CURRENT_TIMESTAMP
WHERE network_id = ? AND id = ?;

-- name: SetPMContactTarget :exec
UPDATE private_message_conversations SET target_user = ?, nickname_key = ? WHERE network_id = ? AND id = ?;

-- name: SetPMConversationOpenByID :exec
UPDATE private_message_conversations SET is_open = ?, updated_at = CURRENT_TIMESTAMP WHERE network_id = ? AND id = ?;

-- name: GetPMConversationByAccount :one
SELECT * FROM private_message_conversations WHERE network_id = ? AND account = ? AND account != '';

-- name: GetUnboundPMConversation :one
SELECT * FROM private_message_conversations
WHERE network_id = ? AND account = '' AND (nickname_key = ? OR LOWER(target_user) = sqlc.arg(target_user))
ORDER BY updated_at DESC, id DESC LIMIT 1;

-- name: BindPMConversationAccount :exec
UPDATE private_message_conversations SET account = ?, nickname_key = ?, identity_source = ?, identity_observed_at = ?
WHERE network_id = ? AND id = ? AND account = '';

-- name: GetUnboundPMContacts :many
SELECT * FROM private_message_conversations WHERE network_id = ? AND account = '';
