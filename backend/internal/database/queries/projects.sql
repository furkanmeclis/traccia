-- name: ProjectInternalWorkspace :one
SELECT id FROM workspaces WHERE uuid=$1 AND status='active';
-- name: LockProjectLead :one
SELECT u.id FROM workspace_members m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND u.uuid=$2 AND m.active AND u.status='active' AND u.deleted_at IS NULL FOR SHARE OF m,u;
-- name: CreateProject :one
INSERT INTO projects(workspace_id,name,summary,lead_user_id,start_date,target_date) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: GetProject :one
SELECT p.*,u.uuid AS lead_user_uuid FROM projects p JOIN workspaces w ON w.id=p.workspace_id LEFT JOIN users u ON u.id=p.lead_user_id WHERE w.uuid=$1 AND p.uuid=$2;
-- name: LockProject :one
SELECT p.* FROM projects p JOIN workspaces w ON w.id=p.workspace_id WHERE w.uuid=$1 AND p.uuid=$2 FOR UPDATE OF p;
-- Cursor anchors identify immutable workspace/parent positions. Only outer rows
-- apply archive filters, so archiving an already returned anchor cannot truncate
-- a later page. A same-scope UUID remains a valid position across filters.
-- name: ListProjects :many
SELECT p.*,u.uuid AS lead_user_uuid FROM projects p JOIN workspaces w ON w.id=p.workspace_id LEFT JOIN users u ON u.id=p.lead_user_id WHERE w.uuid=$1 AND p.archived=$2 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR p.uuid>(SELECT c.uuid FROM projects c WHERE c.workspace_id=p.workspace_id AND c.uuid=sqlc.arg(after_uuid))) ORDER BY p.uuid LIMIT sqlc.arg(row_limit);
-- name: UpdateProject :one
UPDATE projects SET name=$2,summary=$3,lead_user_id=$4,start_date=$5,target_date=$6,version=version+1 WHERE id=$1 AND version=$7 AND NOT archived RETURNING *;
-- name: SetProjectArchived :one
UPDATE projects SET archived=$2,version=version+1 WHERE id=$1 AND version=$3 AND archived<>$2 RETURNING *;
-- name: BumpProject :one
UPDATE projects SET version=version+1 WHERE id=$1 AND version=$2 AND NOT archived RETURNING version;
-- name: DeactivateProjectTeams :exec
UPDATE project_teams SET archived=TRUE WHERE workspace_id=$1 AND project_id=$2 AND NOT archived;
-- name: UpsertProjectTeam :exec
INSERT INTO project_teams(workspace_id,project_id,team_id) VALUES($1,$2,$3) ON CONFLICT(workspace_id,project_id,team_id) DO UPDATE SET archived=FALSE;
-- name: ListProjectTeams :many
SELECT t.* FROM project_teams pt JOIN teams t ON (t.workspace_id,t.id)=(pt.workspace_id,pt.team_id) WHERE pt.workspace_id=$1 AND pt.project_id=$2 AND pt.archived=$3 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR t.uuid>(SELECT ct.uuid FROM project_teams c JOIN teams ct ON (ct.workspace_id,ct.id)=(c.workspace_id,c.team_id) WHERE c.workspace_id=$1 AND c.project_id=$2 AND ct.uuid=sqlc.arg(after_uuid))) ORDER BY t.uuid LIMIT sqlc.arg(row_limit);
-- name: ActiveProjectContribution :one
SELECT pt.id FROM project_teams pt WHERE pt.workspace_id=$1 AND pt.project_id=$2 AND pt.team_id=$3 AND NOT pt.archived FOR SHARE;
-- name: ListProjectMembers :many
SELECT u.uuid,u.name,u.surname,m.role,m.active,(m.active AND u.status='active' AND u.deleted_at IS NULL)::boolean AS assignable FROM workspace_members m JOIN users u ON u.id=m.user_id JOIN workspaces w ON w.id=m.workspace_id WHERE w.uuid=$1 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR u.uuid>(SELECT cu.uuid FROM workspace_members c JOIN users cu ON cu.id=c.user_id WHERE c.workspace_id=m.workspace_id AND cu.uuid=sqlc.arg(after_uuid))) ORDER BY u.uuid LIMIT sqlc.arg(row_limit);
-- name: InsertProjectHistory :exec
INSERT INTO workspace_history(event_id,uuid,workspace_uuid,actor_uuid,event_name,name,version,occurred_at,resource_type,resource_uuid,project_uuid) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);
-- name: CreateProjectMilestone :one
INSERT INTO project_milestones(workspace_id,project_id,name,description,target_date) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: GetProjectMilestone :one
SELECT * FROM project_milestones WHERE workspace_id=$1 AND project_id=$2 AND uuid=$3;
-- name: ListProjectMilestones :many
SELECT s.* FROM project_milestones s WHERE s.workspace_id=$1 AND s.project_id=$2 AND s.archived=$3 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR s.uuid>(SELECT c.uuid FROM project_milestones c WHERE c.workspace_id=$1 AND c.project_id=$2 AND c.uuid=sqlc.arg(after_uuid))) ORDER BY s.uuid LIMIT sqlc.arg(row_limit);
-- name: UpdateProjectMilestone :one
UPDATE project_milestones SET name=$2,description=$3,target_date=$4,version=version+1 WHERE id=$1 AND version=$5 AND NOT archived RETURNING *;
-- name: SetProjectMilestoneArchived :one
UPDATE project_milestones SET archived=$2,version=version+1 WHERE id=$1 AND version=$3 AND archived<>$2 RETURNING *;
-- name: CreateProjectResource :one
INSERT INTO project_resources(workspace_id,project_id,kind,title,url,summary,provider,reference) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;
-- name: GetProjectResource :one
SELECT * FROM project_resources WHERE workspace_id=$1 AND project_id=$2 AND uuid=$3;
-- name: ListProjectResources :many
SELECT s.* FROM project_resources s WHERE s.workspace_id=$1 AND s.project_id=$2 AND s.archived=$3 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR s.uuid>(SELECT c.uuid FROM project_resources c WHERE c.workspace_id=$1 AND c.project_id=$2 AND c.uuid=sqlc.arg(after_uuid))) ORDER BY s.uuid LIMIT sqlc.arg(row_limit);
-- name: UpdateProjectResource :one
UPDATE project_resources SET kind=$2,title=$3,url=$4,summary=$5,provider=$6,reference=$7,version=version+1 WHERE id=$1 AND version=$8 AND NOT archived RETURNING *;
-- name: SetProjectResourceArchived :one
UPDATE project_resources SET archived=$2,version=version+1 WHERE id=$1 AND version=$3 AND archived<>$2 RETURNING *;
-- name: AllProjectContributions :many
SELECT t.*,pt.archived AS contribution_archived FROM project_teams pt JOIN teams t ON (t.workspace_id,t.id)=(pt.workspace_id,pt.team_id) WHERE pt.workspace_id=$1 AND pt.project_id=$2 ORDER BY t.uuid;
