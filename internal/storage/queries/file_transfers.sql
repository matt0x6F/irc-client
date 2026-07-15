-- name: UpsertFileTransfer :exec
INSERT INTO file_transfers (
    transfer_id, network_id, network_name, peer, direction, filename,
    local_path, partial_path, size_bytes, transferred_bytes, state, error,
    resumable, created_at, updated_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(transfer_id) DO UPDATE SET
    network_id = excluded.network_id,
    network_name = excluded.network_name,
    peer = excluded.peer,
    direction = excluded.direction,
    filename = excluded.filename,
    local_path = excluded.local_path,
    partial_path = excluded.partial_path,
    size_bytes = excluded.size_bytes,
    transferred_bytes = excluded.transferred_bytes,
    state = excluded.state,
    error = excluded.error,
    resumable = excluded.resumable,
    updated_at = excluded.updated_at,
    finished_at = excluded.finished_at;

-- name: GetFileTransfer :one
SELECT * FROM file_transfers WHERE transfer_id = ?;

-- name: ListActiveFileTransfers :many
SELECT * FROM file_transfers
WHERE finished_at IS NULL
ORDER BY created_at ASC, transfer_id ASC;

-- name: ListFileTransferHistory :many
SELECT * FROM file_transfers
WHERE finished_at IS NOT NULL
  AND (sqlc.arg(direction) = '' OR direction = sqlc.arg(direction))
  AND (sqlc.arg(search) = ''
       OR LOWER(filename) LIKE '%' || LOWER(sqlc.arg(search)) || '%'
       OR LOWER(peer) LIKE '%' || LOWER(sqlc.arg(search)) || '%'
       OR LOWER(network_name) LIKE '%' || LOWER(sqlc.arg(search)) || '%')
ORDER BY finished_at DESC, transfer_id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListFileTransferHistoryAfter :many
SELECT * FROM file_transfers
WHERE finished_at IS NOT NULL
  AND (finished_at < sqlc.arg(cursor_finished_at)
       OR (finished_at = sqlc.arg(cursor_finished_at) AND transfer_id < sqlc.arg(cursor_transfer_id)))
  AND (sqlc.arg(direction) = '' OR direction = sqlc.arg(direction))
  AND (sqlc.arg(search) = ''
       OR LOWER(filename) LIKE '%' || LOWER(sqlc.arg(search)) || '%'
       OR LOWER(peer) LIKE '%' || LOWER(sqlc.arg(search)) || '%'
       OR LOWER(network_name) LIKE '%' || LOWER(sqlc.arg(search)) || '%')
ORDER BY finished_at DESC, transfer_id DESC
LIMIT sqlc.arg(page_limit);

-- name: DeleteFileTransferHistoryEntry :exec
DELETE FROM file_transfers WHERE transfer_id = ? AND finished_at IS NOT NULL;

-- name: ClearFileTransferHistory :exec
DELETE FROM file_transfers WHERE finished_at IS NOT NULL;

-- name: PruneFileTransferHistory :exec
DELETE FROM file_transfers WHERE finished_at IS NOT NULL AND finished_at < ?;
