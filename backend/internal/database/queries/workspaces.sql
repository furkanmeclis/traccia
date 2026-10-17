-- name: CreateProductWorkspace :one
INSERT INTO workspaces (name) VALUES ($1) RETURNING *;

-- name: AddProductWorkspaceOwner :exec
INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner');

-- name: GetProductActor :one
SELECT id, uuid FROM users WHERE uuid = $1 AND status = 'active' AND deleted_at IS NULL;

-- name: ResolveProductWorkspace :one
SELECT w.*, m.role FROM workspaces w
JOIN workspace_members m ON m.workspace_id = w.id
JOIN users u ON u.id = m.user_id
WHERE w.uuid = sqlc.arg(workspace_uuid) AND u.uuid = sqlc.arg(user_uuid)
AND w.status = 'active' AND m.active = TRUE AND u.status = 'active' AND u.deleted_at IS NULL;

-- name: ListProductWorkspaces :many
SELECT w.* FROM workspaces w JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = sqlc.arg(user_id) AND m.active = TRUE AND w.status = 'active'
AND w.uuid > sqlc.arg(after_uuid)::uuid ORDER BY w.uuid LIMIT sqlc.arg(row_limit);

-- name: UpdateProductWorkspace :one
UPDATE workspaces SET name = sqlc.arg(name), version=version+1 WHERE uuid = sqlc.arg(workspace_uuid) AND version=sqlc.arg(expected_version) AND status='active'
RETURNING *;

-- name: CreateWorkspaceToolCredential :one
INSERT INTO workspace_tool_credentials
(workspace_id, user_id, name, token_hash, audience, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: ResolveWorkspaceToolCredential :one
SELECT c.*, u.uuid AS user_uuid, w.uuid AS workspace_uuid, m.role
FROM workspace_tool_credentials c
JOIN workspaces w ON w.id = c.workspace_id
JOIN workspace_members m ON (m.workspace_id, m.user_id) = (c.workspace_id, c.user_id)
JOIN users u ON u.id = c.user_id
WHERE c.token_hash = $1 AND c.revoked_at IS NULL AND c.expires_at > clock_timestamp()
AND w.status = 'active' AND m.active = TRUE AND u.status = 'active' AND u.deleted_at IS NULL;

-- name: ListWorkspaceToolCredentials :many
SELECT c.* FROM workspace_tool_credentials c JOIN workspaces w ON w.id = c.workspace_id
WHERE w.uuid = sqlc.arg(workspace_uuid) AND c.uuid > sqlc.arg(after_uuid)::uuid
ORDER BY c.uuid LIMIT sqlc.arg(row_limit);

-- name: RevokeWorkspaceToolCredential :execrows
UPDATE workspace_tool_credentials c SET revoked_at = COALESCE(c.revoked_at, NOW())
FROM workspaces w WHERE w.id = c.workspace_id AND w.uuid = sqlc.arg(workspace_uuid)
AND c.uuid = sqlc.arg(credential_uuid);

-- name: GetProductOwner :one
SELECT u.id, u.uuid FROM product_installation i JOIN users u ON u.id=i.owner_user_id
WHERE i.id=TRUE AND u.uuid=$1 AND u.status='active' AND u.deleted_at IS NULL;

-- name: BootstrapProductOwner :execrows
INSERT INTO product_installation (owner_user_id)
SELECT id FROM users WHERE uuid=$1 AND status='active' AND deleted_at IS NULL
ON CONFLICT (id) DO NOTHING;
