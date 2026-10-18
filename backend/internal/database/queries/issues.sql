-- name: ReadIssue :one
SELECT document FROM issue_public WHERE workspace_uuid=$1 AND uuid=$2;
-- name: LockIssue :one
SELECT i.* FROM issues i JOIN workspaces w ON w.id=i.workspace_id WHERE w.uuid=$1 AND i.uuid=$2 FOR UPDATE OF i;
-- name: ResolveIssue :one
SELECT ip.document FROM issue_aliases a JOIN teams t ON (t.workspace_id,t.id)=(a.workspace_id,a.team_id) JOIN issue_public ip ON (ip.workspace_id,ip.id)=(a.workspace_id,a.issue_id) WHERE ip.workspace_uuid=$1 AND t.code=sqlc.arg(team_code) AND a.number=sqlc.arg(number);
-- name: CreateIssue :one
INSERT INTO issues(workspace_id,team_id,number,status_id,title,priority,assignee_id,project_id,milestone_id,start_date,target_date,effort) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;
-- name: UpdateIssue :one
UPDATE issues SET title=$2,priority=$3,status_id=$4,assignee_id=$5,project_id=$6,milestone_id=$7,start_date=$8,target_date=$9,effort=$10,version=version+1 WHERE id=$1 AND version=$11 AND NOT archived RETURNING *;
-- name: MoveIssue :one
UPDATE issues SET team_id=$2,number=$3,status_id=$4,project_id=$5,milestone_id=$6,version=version+1 WHERE id=$1 AND version=$7 AND NOT archived RETURNING *;
-- name: SetIssueArchived :one
UPDATE issues SET archived=$2,version=version+1 WHERE id=$1 AND version=$3 AND archived<>$2 RETURNING *;
-- name: InsertIssueAlias :exec
INSERT INTO issue_aliases(workspace_id,team_id,number,issue_id) VALUES($1,$2,$3,$4);
-- name: ClearIssueLabels :exec
DELETE FROM issue_labels WHERE workspace_id=$1 AND issue_id=$2;
-- name: InsertIssueLabel :exec
INSERT INTO issue_labels(workspace_id,issue_id,label_id) VALUES($1,$2,$3);
-- name: ListIssues :many
SELECT ip.document FROM issue_public ip WHERE workspace_uuid=sqlc.arg(workspace_uuid) AND archived=sqlc.arg(archived)
AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR uuid>sqlc.arg(after_uuid))
AND (sqlc.arg(team_filter)::text='' OR document->>'team_id'=sqlc.arg(team_filter))
AND (sqlc.arg(status_filter)::text='' OR document->>'status_id'=sqlc.arg(status_filter))
AND (sqlc.arg(priority_filter)::text='' OR priority=sqlc.arg(priority_filter))
AND (sqlc.arg(assignee_filter)::text='' OR COALESCE(document->>'assignee_id','unassigned')=sqlc.arg(assignee_filter))
AND (sqlc.arg(project_filter)::text='' OR COALESCE(document->>'project_id','unassigned')=sqlc.arg(project_filter))
AND (sqlc.arg(milestone_filter)::text='' OR COALESCE(document->>'milestone_id','unassigned')=sqlc.arg(milestone_filter))
AND (sqlc.arg(label_filter)::text='' OR (document->'label_ids') ? sqlc.arg(label_filter)::text)
AND (sqlc.arg(search)::text='' OR strpos(lower(title),lower(sqlc.arg(search)))>0 OR strpos(lower(document->>'identifier'),lower(sqlc.arg(search)))>0)
ORDER BY uuid LIMIT sqlc.arg(row_limit);
-- name: InsertIssueHistory :exec
INSERT INTO issue_history(uuid,event_id,workspace_id,issue_id,actor_uuid,event_name,version,occurred_at,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: ListIssueHistory :many
SELECT h.* FROM issue_history h JOIN issues i ON (i.workspace_id,i.id)=(h.workspace_id,h.issue_id) JOIN workspaces w ON w.id=i.workspace_id
WHERE w.uuid=$1 AND i.uuid=$2 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR h.id>(SELECT a.id FROM issue_history a WHERE a.workspace_id=h.workspace_id AND a.issue_id=h.issue_id AND a.uuid=sqlc.arg(after_uuid))) ORDER BY h.id LIMIT sqlc.arg(row_limit);
-- name: HistoricalIssueAssignee :one
SELECT u.id FROM users u JOIN workspace_members m ON m.user_id=u.id JOIN workspaces w ON w.id=m.workspace_id WHERE w.uuid=$1 AND u.uuid=$2;
-- name: IssueHistoryAnchorExists :one
SELECT EXISTS(SELECT 1 FROM issue_history h JOIN issues i ON (i.workspace_id,i.id)=(h.workspace_id,h.issue_id) JOIN workspaces w ON w.id=i.workspace_id WHERE w.uuid=$1 AND i.uuid=$2 AND h.uuid=$3);
-- name: InsertIssueWorkspaceHistory :exec
INSERT INTO workspace_history(event_id,uuid,workspace_uuid,actor_uuid,event_name,name,version,occurred_at,resource_type,resource_uuid,team_uuid,project_uuid,issue_uuid,issue_identifier) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'issues',$9,$10,$11,$9,$12);
