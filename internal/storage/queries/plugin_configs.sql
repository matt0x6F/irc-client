-- name: GetPluginConfig :one
SELECT name, enabled, config, config_schema, created_at, updated_at FROM plugin_configs WHERE name = ?;

-- name: GetAllPluginConfigs :many
SELECT name, enabled, config, config_schema, created_at, updated_at FROM plugin_configs;

-- name: SetPluginEnabled :exec
INSERT INTO plugin_configs (name, enabled, config, config_schema, created_at, updated_at)
VALUES (?, ?, CAST('{}' AS BLOB), CAST('{}' AS BLOB), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET
    enabled = excluded.enabled,
    config = CAST(COALESCE(plugin_configs.config, '{}') AS BLOB),
    config_schema = CAST(COALESCE(plugin_configs.config_schema, '{}') AS BLOB),
    updated_at = CURRENT_TIMESTAMP;

-- name: SetPluginConfig :exec
INSERT INTO plugin_configs (name, enabled, config, config_schema, created_at, updated_at)
VALUES (?, 1, ?, CAST('{}' AS BLOB), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET
    config = excluded.config,
    config_schema = CAST(COALESCE(plugin_configs.config_schema, '{}') AS BLOB),
    updated_at = CURRENT_TIMESTAMP;

-- name: SetPluginConfigSchema :exec
INSERT INTO plugin_configs (name, enabled, config, config_schema, created_at, updated_at)
VALUES (?, 1, CAST('{}' AS BLOB), ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET
    config = CAST(COALESCE(plugin_configs.config, '{}') AS BLOB),
    config_schema = excluded.config_schema,
    updated_at = CURRENT_TIMESTAMP;
