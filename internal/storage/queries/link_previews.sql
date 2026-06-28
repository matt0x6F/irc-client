-- name: GetLinkPreview :one
SELECT url, status, title, description, site_name, image_data, fetched_at
FROM link_previews WHERE url = ?;

-- name: UpsertLinkPreview :exec
INSERT INTO link_previews (url, status, title, description, site_name, image_data, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(url) DO UPDATE SET
    status = excluded.status,
    title = excluded.title,
    description = excluded.description,
    site_name = excluded.site_name,
    image_data = excluded.image_data,
    fetched_at = excluded.fetched_at;

-- name: PruneLinkPreviewsToLimit :exec
DELETE FROM link_previews WHERE url IN (
    SELECT url FROM link_previews
    ORDER BY fetched_at DESC
    LIMIT -1 OFFSET ?
);
