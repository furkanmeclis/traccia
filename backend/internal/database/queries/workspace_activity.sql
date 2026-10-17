-- name: InsertWorkspaceHistory :exec
INSERT INTO workspace_history(event_id,uuid,workspace_uuid,actor_uuid,event_name,name,version,occurred_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: ProjectWorkspaceActivity :execrows
INSERT INTO workspace_activity(id,event_id,workspace_uuid)
SELECT h.id,h.event_id,h.workspace_uuid FROM workspace_history h WHERE h.uuid=$1 AND h.workspace_uuid=$2
ON CONFLICT(event_id) DO NOTHING;

-- name: ListWorkspaceActivity :many
SELECT h.* FROM workspace_activity a JOIN workspace_history h ON h.id=a.id AND h.workspace_uuid=a.workspace_uuid
WHERE a.workspace_uuid=sqlc.arg(workspace_uuid)
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR a.id < (SELECT c.id FROM workspace_history c WHERE c.uuid=sqlc.arg(after_uuid) AND c.workspace_uuid=sqlc.arg(workspace_uuid)))
ORDER BY a.id DESC LIMIT sqlc.arg(row_limit);

-- name: WorkspaceActivityExists :one
SELECT EXISTS(SELECT 1 FROM workspace_activity a JOIN workspace_history h ON h.id=a.id
 WHERE h.uuid=sqlc.arg(uuid) AND a.workspace_uuid=sqlc.arg(workspace_uuid) AND h.workspace_uuid=a.workspace_uuid);
