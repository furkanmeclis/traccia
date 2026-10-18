-- name: LockIssueRelationGraph :exec
SELECT pg_advisory_xact_lock(hashtextextended('traccia.issue-relations:'||sqlc.arg(workspace_uuid)::uuid::text,0));

-- name: ReadRelationIssue :one
SELECT document FROM relation_issue_reference WHERE workspace_uuid=$1 AND uuid=$2;

-- name: ReadRelationState :one
SELECT relation_state(sqlc.arg(workspace_uuid)::uuid,sqlc.arg(issue_uuid)::uuid)::jsonb AS document;

-- name: ReadOutgoingSingleRelation :one
SELECT * FROM issue_relations WHERE workspace_id=$1 AND source_issue_id=$2 AND kind=$3 AND NOT removed;

-- name: ReadActiveIssueRelation :one
SELECT * FROM issue_relations WHERE workspace_id=$1 AND source_issue_id=$2 AND target_issue_id=$3 AND kind=$4 AND NOT removed;

-- name: EnsureIssueRelationRevision :exec
INSERT INTO issue_relation_revisions(workspace_id,issue_id) VALUES($1,$2) ON CONFLICT DO NOTHING;

-- name: LockIssueRelationRevision :one
SELECT version FROM issue_relation_revisions WHERE workspace_id=$1 AND issue_id=$2 FOR UPDATE;

-- name: BumpIssueRelationRevision :exec
UPDATE issue_relation_revisions SET version=version+1 WHERE workspace_id=$1 AND issue_id=$2;

-- name: IssueRelationWouldCycle :one
WITH RECURSIVE reachable AS (
 SELECT sqlc.arg(target_issue_id)::bigint AS issue_id
 UNION SELECT r.target_issue_id FROM issue_relations r JOIN reachable p ON r.source_issue_id=p.issue_id WHERE r.workspace_id=sqlc.arg(workspace_id) AND r.kind=sqlc.arg(kind) AND NOT r.removed
) SELECT EXISTS(SELECT 1 FROM reachable WHERE issue_id=sqlc.arg(source_issue_id)::bigint);

-- name: InsertIssueRelation :one
INSERT INTO issue_relations(workspace_id,source_issue_id,target_issue_id,kind) VALUES($1,$2,$3,$4) RETURNING *;

-- name: RemoveIssueRelation :exec
UPDATE issue_relations SET removed=true,version=2,removed_at=clock_timestamp() WHERE workspace_id=$1 AND uuid=$2 AND NOT removed;

-- name: ReadIssueRelation :one
SELECT document FROM relation_public WHERE workspace_uuid=$1 AND uuid=$2;

-- name: RelationAnchorExists :one
SELECT EXISTS(SELECT 1 FROM relation_public WHERE workspace_uuid=$1 AND uuid=$2 AND (source_uuid=$3 OR target_uuid=$3));

-- name: ListIssueRelations :one
SELECT jsonb_build_object('state',relation_state(sqlc.arg(workspace_uuid)::uuid,sqlc.arg(issue_uuid)::uuid),'items',COALESCE(jsonb_agg(p.document ORDER BY p.uuid),'[]'::jsonb),'has_more',false,'next_cursor','')::jsonb AS document FROM (
 SELECT r.uuid,r.document FROM relation_public r WHERE r.workspace_uuid=sqlc.arg(workspace_uuid) AND (r.source_uuid=sqlc.arg(issue_uuid) OR r.target_uuid=sqlc.arg(issue_uuid))
 AND (sqlc.arg(kind)::text='' OR r.kind=sqlc.arg(kind)) AND r.removed=sqlc.arg(removed)
 AND (sqlc.arg(direction)::text='all' OR (sqlc.arg(direction)='outgoing' AND (r.source_uuid=sqlc.arg(issue_uuid) OR r.kind='related')) OR (sqlc.arg(direction)='incoming' AND (r.target_uuid=sqlc.arg(issue_uuid) OR r.kind='related')))
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR r.uuid>sqlc.arg(after_uuid)) ORDER BY r.uuid LIMIT sqlc.arg(row_limit)
) p;

-- name: InsertIssueRelationHistory :exec
INSERT INTO issue_relation_history(uuid,event_id,workspace_id,issue_id,actor_uuid,event_name,version,occurred_at,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: IssueRelationHistoryAnchorExists :one
SELECT EXISTS(SELECT 1 FROM issue_relation_history h JOIN workspaces w ON w.id=h.workspace_id JOIN issues i ON i.id=h.issue_id AND i.workspace_id=h.workspace_id WHERE w.uuid=$1 AND i.uuid=$2 AND h.uuid=$3);

-- name: ListIssueRelationHistory :many
SELECT h.*,w.uuid AS workspace_uuid,i.uuid AS issue_uuid FROM issue_relation_history h JOIN workspaces w ON w.id=h.workspace_id JOIN issues i ON i.id=h.issue_id AND i.workspace_id=h.workspace_id WHERE w.uuid=sqlc.arg(workspace_uuid) AND i.uuid=sqlc.arg(issue_uuid)
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR h.id>(SELECT a.id FROM issue_relation_history a WHERE a.uuid=sqlc.arg(after_uuid) AND a.workspace_id=h.workspace_id AND a.issue_id=h.issue_id)) ORDER BY h.id LIMIT sqlc.arg(row_limit);

-- name: ReadRelationIdentity :one
SELECT id,workspace_id,uuid FROM relation_issue_reference WHERE workspace_uuid=$1 AND uuid=$2;

-- name: RelationIssueUUID :one
SELECT uuid FROM issues WHERE workspace_id=$1 AND id=$2;
