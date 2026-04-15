-- name: GetPluginConfig :one
SELECT name, enabled, config, config_schema, created_at, updated_at FROM plugin_configs WHERE name = ?;

-- name: GetAllPluginConfigs :many
SELECT name, enabled, config, config_schema, created_at, updated_at FROM plugin_configs;

-- name: SetPluginEnabled :exec
INSERT INTO plugin_configs (name, enabled, created_at, updated_at)
VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET enabled = excluded.enabled, updated_at = CURRENT_TIMESTAMP;

-- name: SetPluginConfig :exec
INSERT INTO plugin_configs (name, enabled, config, created_at, updated_at)
VALUES (?, 1, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET config = excluded.config, updated_at = CURRENT_TIMESTAMP;

-- name: SetPluginConfigSchema :exec
INSERT INTO plugin_configs (name, enabled, config_schema, created_at, updated_at)
VALUES (?, 1, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET config_schema = excluded.config_schema, updated_at = CURRENT_TIMESTAMP;
