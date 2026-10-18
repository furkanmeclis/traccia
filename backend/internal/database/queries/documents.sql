-- name: ReadDocumentOwner :one
SELECT * FROM document_owners WHERE workspace_uuid=$1 AND kind=$2 AND uuid=$3;

-- name: ResolveOwnerDocument :one
SELECT d.* FROM documents d JOIN document_owners o ON o.workspace_id=d.workspace_id AND ((o.kind='issue' AND o.id=d.issue_id) OR (o.kind='project' AND o.id=d.project_id)) WHERE o.workspace_uuid=$1 AND o.kind=$2 AND o.uuid=$3;

-- name: ReadDocument :one
SELECT d.* FROM documents d JOIN workspaces w ON w.id=d.workspace_id WHERE w.uuid=$1 AND d.uuid=$2;

-- name: LockDocument :one
SELECT d.* FROM documents d JOIN workspaces w ON w.id=d.workspace_id WHERE w.uuid=$1 AND d.uuid=$2 FOR UPDATE OF d;

-- name: InsertDocument :one
INSERT INTO documents(uuid,workspace_id,issue_id,project_id,head_revision) VALUES($1,$2,$3,$4,$5) RETURNING *;

-- name: AdvanceDocument :one
UPDATE documents SET head_revision=$3,version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING *;

-- name: ReadDocumentRevision :one
SELECT * FROM document_revisions WHERE workspace_id=$1 AND document_id=$2 AND uuid=$3;

-- name: ReconstructDocumentJournal :many
WITH RECURSIVE journal AS (
 SELECT r.* FROM document_revisions r WHERE r.workspace_id=sqlc.arg(workspace_id) AND r.document_id=sqlc.arg(document_id) AND r.uuid=sqlc.arg(revision_uuid)
 UNION ALL SELECT p.* FROM document_revisions p JOIN journal c ON p.workspace_id=c.workspace_id AND p.document_id=c.document_id AND p.uuid=c.parent_revision WHERE c.checkpoint IS NULL
) SELECT * FROM journal ORDER BY version;

-- name: InsertDocumentRevision :one
INSERT INTO document_revisions(uuid,workspace_id,document_id,parent_revision,source_revision,version,schema_version,kind,checkpoint,patches,actor_uuid,event_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: DocumentRevisionAnchorExists :one
SELECT EXISTS(SELECT 1 FROM document_revisions WHERE workspace_id=$1 AND document_id=$2 AND uuid=$3);

-- name: ListDocumentRevisions :many
SELECT * FROM document_revisions r WHERE r.workspace_id=sqlc.arg(workspace_id) AND r.document_id=sqlc.arg(document_id)
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR r.id>(SELECT a.id FROM document_revisions a WHERE a.workspace_id=r.workspace_id AND a.document_id=r.document_id AND a.uuid=sqlc.arg(after_uuid))) ORDER BY r.id LIMIT sqlc.arg(row_limit);

-- name: ResolveDocumentUserReference :one
SELECT u.id FROM users u JOIN workspace_members m ON m.user_id=u.id JOIN workspaces w ON w.id=m.workspace_id WHERE w.uuid=$1 AND u.uuid=$2;

-- name: ResolveDocumentIssueReference :one
SELECT i.id FROM issues i JOIN workspaces w ON w.id=i.workspace_id WHERE w.uuid=$1 AND i.uuid=$2;

-- name: ResolveDocumentProjectReference :one
SELECT p.id FROM projects p JOIN workspaces w ON w.id=p.workspace_id WHERE w.uuid=$1 AND p.uuid=$2;

-- name: ListCurrentDocumentReferences :many
SELECT * FROM document_reference_public WHERE workspace_id=$1 AND document_id=$2 AND active;

-- name: DeactivateDocumentReference :exec
UPDATE document_references SET active=false WHERE workspace_id=$1 AND uuid=$2 AND active;

-- name: UpdateDocumentReference :exec
UPDATE document_references SET revision_uuid=$3,value=$4 WHERE workspace_id=$1 AND uuid=$2 AND active;

-- name: InsertDocumentReference :exec
INSERT INTO document_references(workspace_id,document_id,node_uuid,revision_uuid,user_id,issue_id,project_id,value) VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: DocumentBacklinkAnchorExists :one
SELECT EXISTS(SELECT 1 FROM document_reference_public WHERE workspace_uuid=$1 AND target_kind=$2 AND target_uuid=$3 AND uuid=$4);

-- name: ListDocumentBacklinks :many
SELECT * FROM document_reference_public WHERE workspace_uuid=sqlc.arg(workspace_uuid) AND target_kind=sqlc.arg(target_kind) AND target_uuid=sqlc.arg(target_uuid) AND active
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR uuid>sqlc.arg(after_uuid)) ORDER BY uuid LIMIT sqlc.arg(row_limit);

-- name: ReadDocumentOwnerByID :one
SELECT * FROM document_owners WHERE workspace_uuid=$1 AND kind=$2 AND id=$3;
