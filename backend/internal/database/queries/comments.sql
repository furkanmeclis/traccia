-- name: ReadComment :one
SELECT d.* FROM issue_comments d JOIN workspaces w ON w.id=d.workspace_id WHERE w.uuid=$1 AND d.uuid=$2;

-- name: LockComment :one
SELECT d.* FROM issue_comments d JOIN workspaces w ON w.id=d.workspace_id WHERE w.uuid=$1 AND d.uuid=$2 FOR UPDATE OF d;

-- name: InsertComment :one
INSERT INTO issue_comments(uuid,workspace_id,issue_id,author_uuid,created_channel,head_revision) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: AdvanceComment :one
UPDATE issue_comments SET head_revision=$3,deleted=$4,version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING *;

-- name: ReadCommentRevision :one
SELECT * FROM comment_revisions WHERE workspace_id=$1 AND comment_id=$2 AND uuid=$3;

-- name: ReconstructCommentJournal :many
WITH RECURSIVE journal AS (
 SELECT r.* FROM comment_revisions r WHERE r.workspace_id=sqlc.arg(workspace_id) AND r.comment_id=sqlc.arg(comment_id) AND r.uuid=sqlc.arg(revision_uuid)
 UNION ALL SELECT p.* FROM comment_revisions p JOIN journal c ON p.workspace_id=c.workspace_id AND p.comment_id=c.comment_id AND p.uuid=c.parent_revision WHERE c.checkpoint IS NULL
) SELECT * FROM journal ORDER BY version;

-- name: InsertCommentRevision :one
INSERT INTO comment_revisions(uuid,workspace_id,comment_id,parent_revision,source_revision,version,schema_version,kind,checkpoint,patches,actor_uuid,event_id,occurred_at,channel,deleted) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING *;

-- name: CommentRevisionAnchorExists :one
SELECT EXISTS(SELECT 1 FROM comment_revisions WHERE workspace_id=$1 AND comment_id=$2 AND uuid=$3);

-- name: ListCommentRevisions :many
SELECT * FROM comment_revisions r WHERE r.workspace_id=sqlc.arg(workspace_id) AND r.comment_id=sqlc.arg(comment_id)
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR r.id>(SELECT a.id FROM comment_revisions a WHERE a.workspace_id=r.workspace_id AND a.comment_id=r.comment_id AND a.uuid=sqlc.arg(after_uuid))) ORDER BY r.id LIMIT sqlc.arg(row_limit);

-- name: ListCurrentCommentReferences :many
SELECT * FROM comment_reference_public WHERE workspace_id=$1 AND comment_id=$2 AND active;

-- name: DeactivateCommentReference :exec
UPDATE comment_references SET active=false WHERE workspace_id=$1 AND uuid=$2 AND active;

-- name: UpdateCommentReference :exec
UPDATE comment_references SET revision_uuid=$3,value=$4 WHERE workspace_id=$1 AND uuid=$2 AND active;

-- name: InsertCommentReference :exec
INSERT INTO comment_references(workspace_id,comment_id,node_uuid,revision_uuid,user_id,issue_id,project_id,value) VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: CommentBacklinkAnchorExists :one
SELECT EXISTS(SELECT 1 FROM comment_reference_public WHERE workspace_uuid=$1 AND target_kind=$2 AND target_uuid=$3 AND uuid=$4);

-- name: ListCommentBacklinks :many
SELECT * FROM comment_reference_public WHERE workspace_uuid=sqlc.arg(workspace_uuid) AND target_kind=sqlc.arg(target_kind) AND target_uuid=sqlc.arg(target_uuid) AND active
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR uuid>sqlc.arg(after_uuid)) ORDER BY uuid LIMIT sqlc.arg(row_limit);


-- name: SetOperationSourceChannel :exec
SELECT set_config('traccia.source_channel',$1::text,true);

-- name: CommentAnchorExists :one
SELECT EXISTS(SELECT 1 FROM issue_comments c JOIN workspaces w ON w.id=c.workspace_id JOIN issues i ON i.workspace_id=c.workspace_id AND i.id=c.issue_id WHERE w.uuid=$1 AND i.uuid=$2 AND c.uuid=$3);

-- name: ListIssueComments :many
SELECT c.* FROM issue_comments c JOIN workspaces w ON w.id=c.workspace_id JOIN issues i ON i.workspace_id=c.workspace_id AND i.id=c.issue_id WHERE w.uuid=sqlc.arg(workspace_uuid) AND i.uuid=sqlc.arg(issue_uuid)
AND (sqlc.arg(include_deleted)::boolean OR NOT c.deleted)
AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR c.id>(SELECT a.id FROM issue_comments a WHERE a.workspace_id=c.workspace_id AND a.issue_id=c.issue_id AND a.uuid=sqlc.arg(after_uuid))) ORDER BY c.id LIMIT sqlc.arg(row_limit);

-- name: TimelineAnchorExists :one
SELECT EXISTS(SELECT 1 FROM issue_timeline_events WHERE workspace_uuid=$1 AND issue_uuid=$2 AND uuid=$3);

-- name: ListIssueTimeline :many
SELECT h.*,ih.snapshot AS issue_snapshot,rh.snapshot AS relation_snapshot,
 dr.uuid AS document_revision,d.uuid AS document_uuid,dr.kind AS document_kind,dr.schema_version AS document_schema_version,
 cr.uuid AS comment_revision,c.uuid AS comment_uuid,cr.kind AS comment_kind,cr.deleted AS comment_deleted,c.author_uuid AS comment_author,c.created_channel AS comment_created_channel,
 ah.snapshot AS attachment_snapshot
 FROM issue_timeline_events h
 LEFT JOIN issue_history ih ON ih.event_id=h.event_id AND ih.workspace_id=h.owning_workspace_id AND ih.issue_id=h.owning_issue_id
 LEFT JOIN issue_relation_history rh ON rh.event_id=h.event_id AND rh.workspace_id=h.owning_workspace_id AND rh.issue_id=h.owning_issue_id
 LEFT JOIN document_revisions dr ON dr.event_id=h.event_id AND dr.workspace_id=h.owning_workspace_id
 LEFT JOIN documents d ON d.workspace_id=dr.workspace_id AND d.id=dr.document_id AND d.issue_id=h.owning_issue_id
 LEFT JOIN comment_revisions cr ON cr.event_id=h.event_id AND cr.workspace_id=h.owning_workspace_id
 LEFT JOIN issue_comments c ON c.workspace_id=cr.workspace_id AND c.id=cr.comment_id AND c.issue_id=h.owning_issue_id
 LEFT JOIN attachment_history ah ON ah.event_id=h.event_id AND ah.workspace_id=h.owning_workspace_id
 WHERE h.workspace_uuid=sqlc.arg(workspace_uuid) AND h.issue_uuid=sqlc.arg(issue_uuid)
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR h.id>(SELECT a.id FROM issue_timeline_events a WHERE a.workspace_uuid=h.workspace_uuid AND a.issue_uuid=h.issue_uuid AND a.uuid=sqlc.arg(after_uuid)))
 ORDER BY h.id LIMIT sqlc.arg(row_limit);
