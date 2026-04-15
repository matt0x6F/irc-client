-- name: GetChannelUsers :many
SELECT * FROM channel_users WHERE channel_id = ? ORDER BY nickname;

-- name: AddChannelUser :exec
INSERT INTO channel_users (channel_id, nickname, modes, created_at, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(channel_id, nickname) DO UPDATE SET modes = excluded.modes, updated_at = CURRENT_TIMESTAMP;

-- name: RemoveChannelUser :exec
DELETE FROM channel_users WHERE channel_id = ? AND LOWER(nickname) = LOWER(?);

-- name: ClearChannelUsers :exec
DELETE FROM channel_users WHERE channel_id = ?;

-- name: ClearNetworkChannelUsers :exec
DELETE FROM channel_users 
WHERE channel_id IN (SELECT id FROM channels WHERE network_id = ?);

-- name: UpdateChannelUserNickname :exec
UPDATE channel_users 
SET nickname = ?, updated_at = CURRENT_TIMESTAMP
WHERE LOWER(nickname) = LOWER(?) 
AND channel_id IN (SELECT id FROM channels WHERE network_id = ?);
