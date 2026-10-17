-- name: CreateTeam :one
INSERT INTO teams(workspace_id,name,code) SELECT id,$2,$3 FROM workspaces w WHERE w.uuid=$1 AND w.status='active' RETURNING *;
-- name: GetTeam :one
SELECT t.* FROM teams t JOIN workspaces w ON w.id=t.workspace_id WHERE w.uuid=$1 AND t.uuid=$2;
-- name: LockTeam :one
SELECT t.* FROM teams t JOIN workspaces w ON w.id=t.workspace_id WHERE w.uuid=$1 AND t.uuid=$2 FOR UPDATE OF t;
-- name: ListTeams :many
SELECT t.* FROM teams t JOIN workspaces w ON w.id=t.workspace_id WHERE w.uuid=$1 AND t.archived=$2 AND ($3::uuid='00000000-0000-0000-0000-000000000000'::uuid OR t.uuid>(SELECT c.uuid FROM teams c WHERE c.workspace_id=t.workspace_id AND c.archived=$2 AND c.uuid=$3)) ORDER BY t.uuid LIMIT $4;
-- name: UpdateTeam :one
UPDATE teams SET name=$2,version=version+1 WHERE id=$1 AND version=$3 AND NOT archived RETURNING *;
-- name: ArchiveTeam :one
UPDATE teams SET archived=TRUE,version=version+1 WHERE id=$1 AND version=$2 AND NOT archived RETURNING *;
-- name: BumpTeamWorkflow :one
UPDATE teams SET workflow_version=workflow_version+1 WHERE id=$1 AND workflow_version=$2 AND NOT archived RETURNING workflow_version;
-- name: AllocateTeamIssueNumber :one
UPDATE teams t SET next_issue_number=next_issue_number+1 FROM workspaces w WHERE w.id=t.workspace_id AND w.uuid=$1 AND t.uuid=$2 AND NOT t.archived AND w.status='active' RETURNING (t.next_issue_number-1)::bigint AS issue_number;
-- name: CreateWorkflowStatus :one
INSERT INTO workflow_statuses(workspace_id,team_id,name,category,color,position) VALUES($1,$2,$3,$4,$5,COALESCE((SELECT max(position)+1 FROM workflow_statuses WHERE team_id=$2),0)) RETURNING *;
-- name: ListWorkflowStatuses :many
SELECT s.* FROM workflow_statuses s WHERE s.team_id=$1 AND s.archived=$2 AND
(sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR s.position>(SELECT c.position FROM workflow_statuses c WHERE c.team_id=$1 AND c.uuid=sqlc.arg(after_uuid) AND c.archived=$2)) ORDER BY s.position LIMIT sqlc.arg(row_limit);
-- name: GetWorkflowStatus :one
SELECT * FROM workflow_statuses WHERE team_id=$1 AND uuid=$2;
-- name: UpdateWorkflowStatus :one
UPDATE workflow_statuses SET name=$2,category=$3,color=$4,version=version+1 WHERE id=$1 AND version=$5 AND NOT archived RETURNING *;
-- name: ArchiveWorkflowStatus :one
UPDATE workflow_statuses SET archived=TRUE,version=version+1 WHERE id=$1 AND version=$2 AND NOT archived RETURNING *;
-- name: RepositionWorkflowStatus :exec
UPDATE workflow_statuses SET position=$2,version=version+1 WHERE id=$1;
-- name: AllWorkflowStatuses :many
SELECT * FROM workflow_statuses WHERE team_id=$1 ORDER BY position;
-- name: CreateLabel :one
INSERT INTO labels(workspace_id,team_id,name,color) VALUES($1,$2,$3,$4) RETURNING *;
-- name: GetLabel :one
SELECT l.*,COALESCE(t.uuid,'00000000-0000-0000-0000-000000000000'::uuid) AS team_uuid FROM labels l JOIN workspaces w ON w.id=l.workspace_id LEFT JOIN teams t ON t.id=l.team_id WHERE w.uuid=$1 AND l.uuid=$2;
-- name: ListLabels :many
SELECT l.*,COALESCE(t.uuid,'00000000-0000-0000-0000-000000000000'::uuid) AS team_uuid FROM labels l JOIN workspaces w ON w.id=l.workspace_id LEFT JOIN teams t ON t.id=l.team_id WHERE w.uuid=$1 AND l.archived=$2 AND ($3::uuid='00000000-0000-0000-0000-000000000000'::uuid OR l.uuid>(SELECT c.uuid FROM labels c WHERE c.workspace_id=l.workspace_id AND c.team_id IS NOT DISTINCT FROM l.team_id AND c.archived=$2 AND c.uuid=$3)) AND COALESCE(t.uuid,'00000000-0000-0000-0000-000000000000'::uuid)=sqlc.arg(team_uuid) ORDER BY l.uuid LIMIT sqlc.arg(row_limit);
-- name: UpdateLabel :one
UPDATE labels SET name=$2,color=$3,version=version+1 WHERE id=$1 AND version=$4 AND NOT archived RETURNING *;
-- name: ArchiveLabel :one
UPDATE labels SET archived=TRUE,version=version+1 WHERE id=$1 AND version=$2 AND NOT archived RETURNING *;
-- name: WorkspaceInternalID :one
SELECT id FROM workspaces WHERE uuid=$1 AND status='active';
-- name: InsertResourceHistory :exec
INSERT INTO workspace_history(event_id,uuid,workspace_uuid,actor_uuid,event_name,name,version,occurred_at,resource_type,resource_uuid,team_uuid) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);

-- name: LockLabel :one
SELECT l.*,COALESCE(t.uuid,'00000000-0000-0000-0000-000000000000'::uuid) AS team_uuid FROM labels l JOIN workspaces w ON w.id=l.workspace_id LEFT JOIN teams t ON t.id=l.team_id WHERE w.uuid=$1 AND l.uuid=$2 FOR SHARE OF l;
