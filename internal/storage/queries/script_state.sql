-- name: UpsertScriptEnabled :exec
INSERT INTO script_state (script_id, enabled)
VALUES (?, ?)
ON CONFLICT(script_id) DO UPDATE SET enabled = excluded.enabled;

-- name: ListDisabledScripts :many
SELECT script_id FROM script_state WHERE enabled = 0;
