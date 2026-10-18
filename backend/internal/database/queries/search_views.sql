-- name: LockSearchRequest :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(identity)::text,0));
-- name: FindSearchRequest :one
SELECT s.*,w.uuid AS workspace_uuid FROM search_query_sessions s JOIN workspaces w ON w.id=s.workspace_id WHERE w.uuid=$1 AND s.actor_uuid=$2 AND s.request_uuid=$3;
-- name: InsertSearchSession :one
INSERT INTO search_query_sessions(workspace_id,actor_uuid,request_uuid,resource,fingerprint,query,first_limit,as_of,expires_at)
SELECT w.id,$2,$3,$4,$5,$6,$7,clock_timestamp(),clock_timestamp()+make_interval(secs=>sqlc.arg(ttl_seconds)::integer) FROM workspaces w WHERE w.uuid=$1 RETURNING *;
-- name: CaptureSearchItems :one
WITH input AS (SELECT sqlc.arg(query)::jsonb AS q),selected AS (
 SELECT src.* FROM search_source_rows src CROSS JOIN input WHERE src.workspace_uuid=sqlc.arg(workspace_uuid) AND src.resource=sqlc.arg(resource)
 AND ((q->>'archived')='any' OR src.archived=((q->>'archived')='archived'))
 AND search_filter_matches(q->'filter',document)
 AND ((q->>'search')='' OR CASE src.resource
 WHEN 'issue' THEN strpos(search_text_fold(document->>'title'),search_text_fold(q->>'search'))>0 OR strpos(search_text_fold(document->>'identifier'),search_text_fold(q->>'search'))>0
 WHEN 'project' THEN strpos(search_text_fold(document->>'name'),search_text_fold(q->>'search'))>0 OR strpos(search_text_fold(document->>'summary'),search_text_fold(q->>'search'))>0
 ELSE strpos(search_text_fold(document->>'name'),search_text_fold(q->>'search'))>0 OR strpos(search_text_fold(document->>'code'),search_text_fold(q->>'search'))>0 END)
),ordered AS (SELECT uuid,document,row_number() OVER(ORDER BY
CASE WHEN (selected.document->>(input.q->'sort'->0->>'field')) IS NULL THEN CASE WHEN (input.q->'sort'->0->>'nulls')='first' THEN 0 ELSE 2 END ELSE 1 END ASC,
CASE WHEN (input.q->'sort'->0->>'direction')='asc' THEN CASE WHEN (input.q->'sort'->0->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->0->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->0->>'field')='effort' THEN (selected.document->>(input.q->'sort'->0->>'field'))::numeric WHEN (input.q->'sort'->0->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->0->>'field'))::timestamptz) END END ASC NULLS LAST,
(CASE WHEN (input.q->'sort'->0->>'direction')='asc' AND (input.q->'sort'->0->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->0->>'field')) END) COLLATE "C" ASC NULLS LAST,
CASE WHEN (input.q->'sort'->0->>'direction')='desc' THEN CASE WHEN (input.q->'sort'->0->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->0->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->0->>'field')='effort' THEN (selected.document->>(input.q->'sort'->0->>'field'))::numeric WHEN (input.q->'sort'->0->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->0->>'field'))::timestamptz) END END DESC NULLS LAST,
(CASE WHEN (input.q->'sort'->0->>'direction')='desc' AND (input.q->'sort'->0->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->0->>'field')) END) COLLATE "C" DESC NULLS LAST,
CASE WHEN (selected.document->>(input.q->'sort'->1->>'field')) IS NULL THEN CASE WHEN (input.q->'sort'->1->>'nulls')='first' THEN 0 ELSE 2 END ELSE 1 END ASC,
CASE WHEN (input.q->'sort'->1->>'direction')='asc' THEN CASE WHEN (input.q->'sort'->1->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->1->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->1->>'field')='effort' THEN (selected.document->>(input.q->'sort'->1->>'field'))::numeric WHEN (input.q->'sort'->1->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->1->>'field'))::timestamptz) END END ASC NULLS LAST,
(CASE WHEN (input.q->'sort'->1->>'direction')='asc' AND (input.q->'sort'->1->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->1->>'field')) END) COLLATE "C" ASC NULLS LAST,
CASE WHEN (input.q->'sort'->1->>'direction')='desc' THEN CASE WHEN (input.q->'sort'->1->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->1->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->1->>'field')='effort' THEN (selected.document->>(input.q->'sort'->1->>'field'))::numeric WHEN (input.q->'sort'->1->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->1->>'field'))::timestamptz) END END DESC NULLS LAST,
(CASE WHEN (input.q->'sort'->1->>'direction')='desc' AND (input.q->'sort'->1->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->1->>'field')) END) COLLATE "C" DESC NULLS LAST,
CASE WHEN (selected.document->>(input.q->'sort'->2->>'field')) IS NULL THEN CASE WHEN (input.q->'sort'->2->>'nulls')='first' THEN 0 ELSE 2 END ELSE 1 END ASC,
CASE WHEN (input.q->'sort'->2->>'direction')='asc' THEN CASE WHEN (input.q->'sort'->2->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->2->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->2->>'field')='effort' THEN (selected.document->>(input.q->'sort'->2->>'field'))::numeric WHEN (input.q->'sort'->2->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->2->>'field'))::timestamptz) END END ASC NULLS LAST,
(CASE WHEN (input.q->'sort'->2->>'direction')='asc' AND (input.q->'sort'->2->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->2->>'field')) END) COLLATE "C" ASC NULLS LAST,
CASE WHEN (input.q->'sort'->2->>'direction')='desc' THEN CASE WHEN (input.q->'sort'->2->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->2->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->2->>'field')='effort' THEN (selected.document->>(input.q->'sort'->2->>'field'))::numeric WHEN (input.q->'sort'->2->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->2->>'field'))::timestamptz) END END DESC NULLS LAST,
(CASE WHEN (input.q->'sort'->2->>'direction')='desc' AND (input.q->'sort'->2->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->2->>'field')) END) COLLATE "C" DESC NULLS LAST,
CASE WHEN (selected.document->>(input.q->'sort'->3->>'field')) IS NULL THEN CASE WHEN (input.q->'sort'->3->>'nulls')='first' THEN 0 ELSE 2 END ELSE 1 END ASC,
CASE WHEN (input.q->'sort'->3->>'direction')='asc' THEN CASE WHEN (input.q->'sort'->3->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->3->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->3->>'field')='effort' THEN (selected.document->>(input.q->'sort'->3->>'field'))::numeric WHEN (input.q->'sort'->3->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->3->>'field'))::timestamptz) END END ASC NULLS LAST,
(CASE WHEN (input.q->'sort'->3->>'direction')='asc' AND (input.q->'sort'->3->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->3->>'field')) END) COLLATE "C" ASC NULLS LAST,
CASE WHEN (input.q->'sort'->3->>'direction')='desc' THEN CASE WHEN (input.q->'sort'->3->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->3->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->3->>'field')='effort' THEN (selected.document->>(input.q->'sort'->3->>'field'))::numeric WHEN (input.q->'sort'->3->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->3->>'field'))::timestamptz) END END DESC NULLS LAST,
(CASE WHEN (input.q->'sort'->3->>'direction')='desc' AND (input.q->'sort'->3->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->3->>'field')) END) COLLATE "C" DESC NULLS LAST,
CASE WHEN (selected.document->>(input.q->'sort'->4->>'field')) IS NULL THEN CASE WHEN (input.q->'sort'->4->>'nulls')='first' THEN 0 ELSE 2 END ELSE 1 END ASC,
CASE WHEN (input.q->'sort'->4->>'direction')='asc' THEN CASE WHEN (input.q->'sort'->4->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->4->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->4->>'field')='effort' THEN (selected.document->>(input.q->'sort'->4->>'field'))::numeric WHEN (input.q->'sort'->4->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->4->>'field'))::timestamptz) END END ASC NULLS LAST,
(CASE WHEN (input.q->'sort'->4->>'direction')='asc' AND (input.q->'sort'->4->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->4->>'field')) END) COLLATE "C" ASC NULLS LAST,
CASE WHEN (input.q->'sort'->4->>'direction')='desc' THEN CASE WHEN (input.q->'sort'->4->>'field')='priority' THEN CASE (selected.document->>(input.q->'sort'->4->>'field')) WHEN 'none' THEN 0 WHEN 'low' THEN 1 WHEN 'normal' THEN 2 WHEN 'high' THEN 3 WHEN 'urgent' THEN 4 END WHEN (input.q->'sort'->4->>'field')='effort' THEN (selected.document->>(input.q->'sort'->4->>'field'))::numeric WHEN (input.q->'sort'->4->>'field') IN ('created_at','updated_at') THEN extract(epoch FROM (selected.document->>(input.q->'sort'->4->>'field'))::timestamptz) END END DESC NULLS LAST,
(CASE WHEN (input.q->'sort'->4->>'direction')='desc' AND (input.q->'sort'->4->>'field') NOT IN ('effort','priority','created_at','updated_at') THEN (selected.document->>(input.q->'sort'->4->>'field')) END) COLLATE "C" DESC NULLS LAST,
selected.uuid ASC) AS ordinal FROM selected CROSS JOIN input),inserted AS (
 INSERT INTO search_query_items(workspace_id,session_id,ordinal,resource_uuid,snapshot)
 SELECT sqlc.arg(workspace_id)::bigint,sqlc.arg(session_id)::bigint,ordinal,uuid,document FROM ordered RETURNING id
) SELECT count(*)::bigint AS total_count,statement_timestamp()::timestamptz AS as_of FROM inserted;
-- name: PublishSearchSession :one
UPDATE search_query_sessions SET state='ready',total_count=$2,as_of=$3,expires_at=$3::timestamptz+make_interval(secs=>sqlc.arg(ttl_seconds)::integer),updated_at=clock_timestamp() WHERE id=$1 AND state='pending' RETURNING *;
-- name: ReadSearchSession :one
SELECT s.*,w.uuid AS workspace_uuid,(s.expires_at<=clock_timestamp())::boolean AS expired FROM search_query_sessions s JOIN workspaces w ON w.id=s.workspace_id WHERE w.uuid=$1 AND s.actor_uuid=$2 AND s.uuid=$3 AND s.resource=$4 AND s.state IN ('ready','expired') FOR SHARE OF s;
-- name: SearchSessionExpired :one
SELECT expires_at<=clock_timestamp() FROM search_query_sessions WHERE id=$1;
-- name: ListSearchItems :many
SELECT ordinal,snapshot FROM search_query_items WHERE session_id=$1 AND workspace_id=$2 AND ordinal>sqlc.arg(after_ordinal) ORDER BY ordinal LIMIT sqlc.arg(row_limit);
-- name: SearchItemAnchorExists :one
SELECT EXISTS(SELECT 1 FROM search_query_items WHERE session_id=$1 AND workspace_id=$2 AND ordinal=$3);
-- name: CleanupSearchItems :execrows
WITH expired AS (SELECT s.id FROM search_query_sessions s WHERE s.state='ready' AND s.expires_at<=clock_timestamp() AND EXISTS(SELECT 1 FROM search_query_items i WHERE i.session_id=s.id) ORDER BY s.id LIMIT 10 FOR UPDATE OF s SKIP LOCKED)
,doomed AS (SELECT i.id FROM search_query_items i JOIN expired e ON e.id=i.session_id ORDER BY i.id LIMIT sqlc.arg(row_limit))
DELETE FROM search_query_items WHERE id IN (SELECT id FROM doomed);
-- name: InsertSavedView :one
INSERT INTO saved_views(workspace_id,actor_uuid,revision,name,resource,definition,columns)
SELECT w.id,$2,$3,$4,$5,$6,$7 FROM workspaces w WHERE w.uuid=$1 RETURNING *;
-- name: ReadSavedView :one
SELECT v.* FROM saved_views v JOIN workspaces w ON w.id=v.workspace_id WHERE w.uuid=$1 AND v.actor_uuid=$2 AND v.uuid=$3;
-- name: LockSavedView :one
SELECT v.* FROM saved_views v JOIN workspaces w ON w.id=v.workspace_id WHERE w.uuid=$1 AND v.actor_uuid=$2 AND v.uuid=$3 FOR UPDATE OF v;
-- name: AdvanceSavedView :one
UPDATE saved_views SET revision=$2,name=$3,resource=$4,definition=$5,columns=$6,deleted=$7,version=version+1 WHERE id=$1 RETURNING *;
-- name: ListSavedViews :many
SELECT v.* FROM saved_views v JOIN workspaces w ON w.id=v.workspace_id WHERE w.uuid=$1 AND v.actor_uuid=$2
 AND (sqlc.arg(resource)::text='any' OR v.resource=sqlc.arg(resource)) AND (sqlc.arg(include_deleted)::boolean OR NOT v.deleted)
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR v.uuid>sqlc.arg(after_uuid)) ORDER BY v.uuid LIMIT sqlc.arg(row_limit);
-- name: SavedViewAnchorExists :one
SELECT EXISTS(SELECT 1 FROM saved_views v JOIN workspaces w ON w.id=v.workspace_id WHERE w.uuid=$1 AND v.actor_uuid=$2 AND v.uuid=$3);
-- name: ReadColumnPreferences :one
SELECT c.* FROM column_preferences c JOIN workspaces w ON w.id=c.workspace_id WHERE w.uuid=$1 AND c.actor_uuid=$2 AND c.resource=$3;
-- name: LockColumnPreferences :one
SELECT c.* FROM column_preferences c JOIN workspaces w ON w.id=c.workspace_id WHERE w.uuid=$1 AND c.actor_uuid=$2 AND c.resource=$3 FOR UPDATE OF c;
-- name: InsertColumnPreferences :one
INSERT INTO column_preferences(workspace_id,actor_uuid,resource,revision,columns)
SELECT w.id,$2,$3,$4,$5 FROM workspaces w WHERE w.uuid=$1 ON CONFLICT(workspace_id,actor_uuid,resource) DO NOTHING RETURNING *;
-- name: AdvanceColumnPreferences :one
UPDATE column_preferences SET revision=$2,columns=$3,version=version+1 WHERE id=$1 RETURNING *;
-- name: InsertPrivatePreferenceHistory :exec
INSERT INTO private_preference_history(workspace_id,actor_uuid,view_id,column_preference_id,revision,version,kind,snapshot,event_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);
-- name: InsertPreferenceWorkspaceHistory :exec
INSERT INTO workspace_history(event_id,uuid,workspace_uuid,actor_uuid,event_name,name,version,occurred_at,resource_type,resource_uuid) VALUES($1,$2,$3,$4,$5,'Private preference',$6,$7,$8,$9);

-- name: CompactSearchSessions :execrows
WITH drained AS (SELECT s.id FROM search_query_sessions s WHERE s.state='ready' AND s.expires_at<=clock_timestamp() AND NOT EXISTS(SELECT 1 FROM search_query_items i WHERE i.session_id=s.id) ORDER BY s.id LIMIT 10 FOR UPDATE OF s SKIP LOCKED)
UPDATE search_query_sessions SET state='expired',query=NULL,updated_at=clock_timestamp() WHERE id IN (SELECT id FROM drained);
