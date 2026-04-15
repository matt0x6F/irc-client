-- name: CreateChannel :one
INSERT INTO channels (network_id, name, auto_join, is_open, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetChannels :many
SELECT * FROM channels WHERE network_id = ? ORDER BY name;

-- name: GetJoinedChannels :many
SELECT DISTINCT c.* 
FROM channels c
INNER JOIN channel_users cu ON c.id = cu.channel_id
WHERE c.network_id = ? AND LOWER(cu.nickname) = LOWER(?)
ORDER BY c.name;

-- name: GetChannelByName :one
SELECT * FROM channels WHERE network_id = ? AND LOWER(name) = LOWER(?);

-- name: UpdateChannelTopic :exec
UPDATE channels SET topic = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: UpdateChannelModes :exec
UPDATE channels SET modes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: UpdateChannelAutoJoin :exec
UPDATE channels SET auto_join = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: UpdateChannelIsOpen :exec
UPDATE channels SET is_open = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: GetOpenChannels :many
SELECT DISTINCT c.* 
FROM channels c
LEFT JOIN channel_users cu ON c.id = cu.channel_id AND LOWER(cu.nickname) = LOWER(?)
WHERE c.network_id = ? AND (c.is_open = 1 OR cu.nickname IS NOT NULL)
ORDER BY c.name;
