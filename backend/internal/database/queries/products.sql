-- name: ProductInternalWorkspace :one
SELECT id FROM workspaces WHERE uuid=$1 AND status='active';
-- name: CreateProduct :one
INSERT INTO products(workspace_id,name,summary,repository_url,repository_provider,repository_reference) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: GetProduct :one
SELECT p.* FROM products p JOIN workspaces w ON w.id=p.workspace_id WHERE w.uuid=$1 AND p.uuid=$2;
-- name: LockProduct :one
SELECT p.* FROM products p JOIN workspaces w ON w.id=p.workspace_id WHERE w.uuid=$1 AND p.uuid=$2 FOR UPDATE OF p;
-- name: ListProducts :many
SELECT p.* FROM products p JOIN workspaces w ON w.id=p.workspace_id
WHERE w.uuid=sqlc.arg(workspace_uuid) AND p.archived=sqlc.arg(archived)
 AND p.uuid>sqlc.arg(after_uuid)::uuid
 AND (sqlc.arg(search)::text='' OR strpos(lower(p.name || ' ' || p.summary),lower(sqlc.arg(search)::text))>0)
 AND (sqlc.arg(project_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR EXISTS(SELECT 1 FROM product_projects pp JOIN projects pr ON (pr.workspace_id,pr.id)=(pp.workspace_id,pp.project_id) WHERE pp.workspace_id=p.workspace_id AND pp.product_id=p.id AND NOT pp.archived AND pr.uuid=sqlc.arg(project_uuid)))
ORDER BY p.uuid LIMIT sqlc.arg(row_limit);
-- name: UpdateProduct :one
UPDATE products SET name=$2,summary=$3,repository_url=$4,repository_provider=$5,repository_reference=$6,version=version+1 WHERE id=$1 AND version=$7 AND NOT archived RETURNING *;
-- name: SetProductArchived :one
UPDATE products SET archived=$2,version=version+1 WHERE id=$1 AND version=$3 AND archived<>$2 RETURNING *;
-- name: BumpProduct :one
UPDATE products SET version=version+1 WHERE id=$1 AND version=$2 AND NOT archived RETURNING version;
-- name: DeactivateProductProjects :exec
UPDATE product_projects SET archived=TRUE WHERE workspace_id=$1 AND product_id=$2 AND NOT archived;
-- name: UpsertProductProject :exec
INSERT INTO product_projects(workspace_id,product_id,project_id) VALUES($1,$2,$3) ON CONFLICT(workspace_id,product_id,project_id) DO UPDATE SET archived=FALSE;
-- name: AllProductProjects :many
SELECT p.*,u.uuid AS lead_user_uuid,pp.archived AS association_archived FROM product_projects pp JOIN projects p ON (p.workspace_id,p.id)=(pp.workspace_id,pp.project_id) LEFT JOIN users u ON u.id=p.lead_user_id WHERE pp.workspace_id=$1 AND pp.product_id=$2 ORDER BY p.uuid;
-- name: ListProductProjects :many
SELECT p.*,u.uuid AS lead_user_uuid FROM product_projects pp JOIN projects p ON (p.workspace_id,p.id)=(pp.workspace_id,pp.project_id) LEFT JOIN users u ON u.id=p.lead_user_id WHERE pp.workspace_id=$1 AND pp.product_id=$2 AND pp.archived=$3 AND p.uuid>sqlc.arg(after_uuid)::uuid ORDER BY p.uuid LIMIT sqlc.arg(row_limit);
-- name: InsertProductHistory :exec
INSERT INTO workspace_history(event_id,uuid,workspace_uuid,actor_uuid,event_name,name,version,occurred_at,resource_type,resource_uuid,product_uuid,environment_uuid) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);
-- name: CreateProductEnvironment :one
INSERT INTO product_environments(workspace_id,product_id,name,kind,url,deploy_url,deploy_provider,deploy_reference,summary) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;
-- name: GetProductEnvironment :one
SELECT * FROM product_environments WHERE workspace_id=$1 AND product_id=$2 AND uuid=$3;
-- name: ListProductEnvironments :many
SELECT e.* FROM product_environments e WHERE e.workspace_id=$1 AND e.product_id=$2 AND e.archived=$3 AND e.uuid>sqlc.arg(after_uuid)::uuid
 AND (sqlc.arg(search)::text='' OR strpos(lower(e.name || ' ' || e.summary),lower(sqlc.arg(search)::text))>0)
 AND (sqlc.arg(kind)::text='' OR e.kind=sqlc.arg(kind))
ORDER BY e.uuid LIMIT sqlc.arg(row_limit);
-- name: UpdateProductEnvironment :one
UPDATE product_environments SET name=$2,kind=$3,url=$4,deploy_url=$5,deploy_provider=$6,deploy_reference=$7,summary=$8,version=version+1 WHERE id=$1 AND version=$9 AND NOT archived RETURNING *;
-- name: SetProductEnvironmentArchived :one
UPDATE product_environments SET archived=$2,version=version+1 WHERE id=$1 AND version=$3 AND archived<>$2 RETURNING *;

-- name: ProductProjectAnchorExists :one
SELECT EXISTS(SELECT 1 FROM product_projects pp JOIN projects p ON (p.workspace_id,p.id)=(pp.workspace_id,pp.project_id) WHERE pp.workspace_id=$1 AND pp.product_id=$2 AND p.uuid=$3);
