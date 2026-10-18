-- name: CreateAttachmentUpload :one
INSERT INTO attachment_uploads(workspace_id,actor_id,draft_uuid,filename,content_type,size_bytes,sha256,chunk_bytes,expires_at) SELECT w.id,a.id,$3,$4,$5,$6,$7,$8,$9 FROM workspaces w JOIN users a ON a.uuid=$2 WHERE w.uuid=$1 RETURNING *;
-- name: ReadOwnedAttachmentUpload :one
SELECT * FROM attachment_upload_public WHERE workspace_uuid=$1 AND actor_uuid=$2 AND uuid=$3 AND draft_uuid=$4;
-- name: LockOwnedAttachmentUpload :one
SELECT u.* FROM attachment_uploads u JOIN workspaces w ON w.id=u.workspace_id JOIN users a ON a.id=u.actor_id WHERE w.uuid=$1 AND a.uuid=$2 AND u.uuid=$3 AND u.draft_uuid=$4 FOR UPDATE OF u;
-- name: LockAttachmentUpload :one
SELECT * FROM attachment_uploads WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: ReadAttachmentUpload :one
SELECT * FROM attachment_upload_public WHERE workspace_id=$1 AND id=$2;
-- name: AdvanceAttachmentUploadChunk :one
UPDATE attachment_uploads SET next_offset=$3,version=version+1,expires_at=$4 WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: FinalizeAttachmentUpload :one
UPDATE attachment_uploads SET attachment_id=$3,state='finalized',version=version+1,expires_at=$4,cleanup_state='pending',next_cleanup_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: BindAttachmentUpload :one
UPDATE attachment_uploads SET state='bound',version=version+1,expires_at=NULL WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: RemoveBoundAttachmentUpload :one
UPDATE attachment_uploads SET state='removed',version=version+1 WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: TombstoneAttachmentUpload :one
UPDATE attachment_uploads SET state=$3,version=version+1,cleanup_state='pending',next_cleanup_at=clock_timestamp(),last_cleanup_error='' WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: InsertAttachmentChunk :exec
INSERT INTO attachment_chunks(workspace_id,upload_id,offset_bytes,size_bytes,sha256,object_key) VALUES($1,$2,$3,$4,$5,$6);
-- name: ListAttachmentChunks :many
SELECT * FROM attachment_chunks WHERE workspace_id=$1 AND upload_id=$2 ORDER BY offset_bytes;
-- name: ReadAttachmentChunkRange :many
SELECT * FROM attachment_chunks WHERE workspace_id=$1 AND upload_id=$2 AND offset_bytes<sqlc.arg(end_offset)::bigint AND offset_bytes+size_bytes>sqlc.arg(start_offset)::bigint ORDER BY offset_bytes;
-- name: CreateAttachment :one
INSERT INTO attachments(workspace_id,upload_id,actor_id,draft_uuid,filename,content_type,size_bytes,sha256,manifest_key,expires_at) SELECT u.workspace_id,u.id,u.actor_id,u.draft_uuid,u.filename,u.content_type,u.size_bytes,u.sha256,$3,$4 FROM attachment_uploads u WHERE u.workspace_id=$1 AND u.id=$2 RETURNING *;
-- name: ReadAttachment :one
SELECT * FROM attachment_public WHERE workspace_uuid=$1 AND uuid=$2;
-- name: LockAttachment :one
SELECT * FROM attachments WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: BindAttachment :one
UPDATE attachments SET issue_id=$3,version=version+1,expires_at=NULL WHERE workspace_id=$1 AND id=$2 AND issue_id IS NULL AND NOT removed RETURNING *;
-- name: RemoveAttachment :one
UPDATE attachments SET removed=TRUE,version=version+1 WHERE workspace_id=$1 AND id=$2 AND issue_id IS NOT NULL AND NOT removed RETURNING *;
-- name: AttachmentUploadAnchorExists :one
SELECT EXISTS(SELECT 1 FROM attachment_upload_public WHERE workspace_uuid=$1 AND actor_uuid=$2 AND uuid=$3 AND (sqlc.arg(draft_filter)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR draft_uuid=sqlc.arg(draft_filter)));
-- name: ListAttachmentUploads :many
SELECT * FROM attachment_upload_public WHERE workspace_uuid=sqlc.arg(workspace_uuid) AND actor_uuid=sqlc.arg(actor_uuid) AND uuid>sqlc.arg(after_uuid)::uuid AND (sqlc.arg(draft_filter)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR draft_uuid=sqlc.arg(draft_filter)) AND (sqlc.arg(state_filter)::text='any' OR CASE WHEN state IN ('uploading','finalized') AND expires_at<=clock_timestamp() THEN 'expired' ELSE state END=sqlc.arg(state_filter)) ORDER BY uuid LIMIT sqlc.arg(row_limit);
-- name: AttachmentAnchorExists :one
SELECT EXISTS(SELECT 1 FROM attachment_public WHERE workspace_uuid=$1 AND issue_uuid=$2 AND uuid=$3);
-- name: ListIssueAttachments :many
SELECT * FROM attachment_public WHERE workspace_uuid=sqlc.arg(workspace_uuid) AND issue_uuid=sqlc.arg(issue_uuid) AND removed=sqlc.arg(removed) AND strpos(lower(filename),lower(sqlc.arg(search)::text))>0 AND uuid>sqlc.arg(after_uuid)::uuid ORDER BY uuid LIMIT sqlc.arg(row_limit);
-- name: InsertAttachmentUploadHistory :exec
INSERT INTO attachment_upload_history(uuid,event_id,workspace_id,upload_id,actor_uuid,event_name,version,occurred_at,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: InsertAttachmentHistory :exec
INSERT INTO attachment_history(uuid,event_id,workspace_id,attachment_id,actor_uuid,event_name,version,occurred_at,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: AttachmentUploadHistoryAnchorExists :one
SELECT EXISTS(SELECT 1 FROM attachment_upload_history WHERE workspace_id=$1 AND upload_id=$2 AND uuid=$3);
-- name: ListAttachmentUploadHistory :many
SELECT * FROM attachment_upload_history h WHERE h.workspace_id=sqlc.arg(workspace_id) AND h.upload_id=sqlc.arg(upload_id) AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR h.id>(SELECT a.id FROM attachment_upload_history a WHERE a.workspace_id=h.workspace_id AND a.upload_id=h.upload_id AND a.uuid=sqlc.arg(after_uuid))) ORDER BY h.id LIMIT sqlc.arg(row_limit);
-- name: AttachmentHistoryAnchorExists :one
SELECT EXISTS(SELECT 1 FROM attachment_history WHERE workspace_id=$1 AND attachment_id=$2 AND uuid=$3);
-- name: ListAttachmentHistory :many
SELECT * FROM attachment_history h WHERE h.workspace_id=sqlc.arg(workspace_id) AND h.attachment_id=sqlc.arg(attachment_id) AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR h.id>(SELECT a.id FROM attachment_history a WHERE a.workspace_id=h.workspace_id AND a.attachment_id=h.attachment_id AND a.uuid=sqlc.arg(after_uuid))) ORDER BY h.id LIMIT sqlc.arg(row_limit);
-- name: ClaimExpiredAttachmentUploads :many
SELECT * FROM attachment_uploads WHERE state IN ('uploading','finalized') AND expires_at<=clock_timestamp() ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT $1;
-- name: ClaimAttachmentCleanup :many
SELECT * FROM attachment_uploads WHERE state<>'uploading' AND next_cleanup_at<=clock_timestamp() ORDER BY next_cleanup_at,id FOR UPDATE SKIP LOCKED LIMIT $1;
-- name: ScheduleAttachmentCleanup :exec
UPDATE attachment_uploads SET next_cleanup_at=$3,cleanup_state=$4,last_cleanup_error=$5,cleanup_passes=cleanup_passes+1 WHERE workspace_id=$1 AND id=$2 AND state<>'uploading';

-- name: InsertAttachmentFileChunk :exec
INSERT INTO attachment_file_chunks(workspace_id,attachment_id,upload_chunk_id,object_key) VALUES($1,$2,$3,$4);
-- name: ReadFinalAttachmentChunkRange :many
SELECT c.offset_bytes,c.size_bytes,c.sha256,f.object_key FROM attachment_file_chunks f JOIN attachment_chunks c ON c.workspace_id=f.workspace_id AND c.id=f.upload_chunk_id WHERE f.workspace_id=$1 AND f.attachment_id=$2 AND c.offset_bytes<sqlc.arg(end_offset)::bigint AND c.offset_bytes+c.size_bytes>sqlc.arg(start_offset)::bigint ORDER BY c.offset_bytes;
