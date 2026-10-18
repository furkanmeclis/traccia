CREATE TABLE attachment_uploads (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),actor_id BIGINT NOT NULL,draft_uuid UUID NOT NULL CHECK(draft_uuid<>'00000000-0000-0000-0000-000000000000'),
 filename TEXT NOT NULL CHECK(length(filename) BETWEEN 1 AND 255),content_type TEXT NOT NULL CHECK(length(content_type)<=255),
 size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 0 AND 268435456),sha256 TEXT NOT NULL CHECK(sha256~'^[a-f0-9]{64}$'),
 chunk_bytes INTEGER NOT NULL CHECK(chunk_bytes BETWEEN 1 AND 262144),next_offset BIGINT NOT NULL DEFAULT 0,version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 state TEXT NOT NULL DEFAULT 'uploading' CHECK(state IN ('uploading','finalized','bound','removed','aborted','expired')),
 attachment_id BIGINT,expires_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 cleanup_state TEXT NOT NULL DEFAULT 'none' CHECK(cleanup_state IN ('none','pending','retrying','reconciled')),
 next_cleanup_at TIMESTAMPTZ,last_cleanup_error TEXT NOT NULL DEFAULT '',cleanup_passes BIGINT NOT NULL DEFAULT 0 CHECK(cleanup_passes>=0),
 UNIQUE(workspace_id,id),UNIQUE(workspace_id,id,actor_id,draft_uuid),
 FOREIGN KEY(workspace_id,actor_id) REFERENCES workspace_members(workspace_id,user_id),
 CHECK(next_offset BETWEEN 0 AND size_bytes),CHECK(size_bytes<=chunk_bytes::bigint*4096),
 CHECK((state IN ('bound','removed'))=(expires_at IS NULL)),
 CHECK((state IN ('finalized','bound','removed'))=(attachment_id IS NOT NULL) OR state IN ('aborted','expired')),
 CHECK(state NOT IN ('finalized','bound','removed') OR next_offset=size_bytes),
 CHECK((state<>'uploading')=(next_cleanup_at IS NOT NULL)),
 CHECK((cleanup_state='none')=(state='uploading'))
);
CREATE INDEX idx_attachment_upload_actor_page ON attachment_uploads(workspace_id,actor_id,uuid);
CREATE INDEX idx_attachment_upload_expiry ON attachment_uploads(expires_at,id) WHERE state IN ('uploading','finalized');
CREATE INDEX idx_attachment_cleanup_due ON attachment_uploads(next_cleanup_at,id) WHERE state IN ('aborted','expired');
CREATE TRIGGER trg_attachment_upload_updated_at BEFORE UPDATE ON attachment_uploads FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_attachment_upload() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'upload tombstones are permanent';END IF;
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.actor_id,NEW.draft_uuid,NEW.filename,NEW.content_type,NEW.size_bytes,NEW.sha256,NEW.chunk_bytes,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.actor_id,OLD.draft_uuid,OLD.filename,OLD.content_type,OLD.size_bytes,OLD.sha256,OLD.chunk_bytes,OLD.created_at) THEN RAISE EXCEPTION 'upload identity is immutable';END IF;
 IF OLD.attachment_id IS NOT NULL AND NEW.attachment_id IS DISTINCT FROM OLD.attachment_id THEN RAISE EXCEPTION 'finalized file identity is immutable';END IF;
 IF NOT ((OLD.state='uploading' AND NEW.state IN ('uploading','finalized','aborted','expired')) OR (OLD.state='finalized' AND NEW.state IN ('finalized','bound','aborted','expired')) OR (OLD.state='bound' AND NEW.state IN ('bound','removed')) OR (OLD.state IN ('removed','aborted','expired') AND NEW.state=OLD.state)) THEN RAISE EXCEPTION 'invalid upload transition';END IF;
 IF NEW.next_offset<OLD.next_offset OR (OLD.state<>'uploading' AND NEW.next_offset<>OLD.next_offset) THEN RAISE EXCEPTION 'upload progress is immutable';END IF;
 IF ROW(NEW.state,NEW.next_offset,NEW.attachment_id,NEW.expires_at) IS DISTINCT FROM ROW(OLD.state,OLD.next_offset,OLD.attachment_id,OLD.expires_at) THEN
  IF NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'upload version must advance';END IF;
 ELSIF NEW.version<>OLD.version THEN RAISE EXCEPTION 'cleanup cannot alter upload version';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_attachment_upload_identity BEFORE UPDATE OR DELETE ON attachment_uploads FOR EACH ROW EXECUTE FUNCTION protect_attachment_upload();
CREATE TABLE attachments (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL,upload_id BIGINT NOT NULL,actor_id BIGINT NOT NULL,draft_uuid UUID NOT NULL,
 file_revision UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 filename TEXT NOT NULL,content_type TEXT NOT NULL,size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 0 AND 268435456),sha256 TEXT NOT NULL CHECK(sha256~'^[a-f0-9]{64}$'),manifest_key TEXT NOT NULL UNIQUE,
 issue_id BIGINT,removed BOOLEAN NOT NULL DEFAULT FALSE,finalized_at TIMESTAMPTZ NOT NULL DEFAULT now(),expires_at TIMESTAMPTZ,
 UNIQUE(workspace_id,id),UNIQUE(workspace_id,upload_id),UNIQUE(workspace_id,id,upload_id),
 FOREIGN KEY(workspace_id,upload_id,actor_id,draft_uuid) REFERENCES attachment_uploads(workspace_id,id,actor_id,draft_uuid),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id),
 CHECK((issue_id IS NOT NULL)=(expires_at IS NULL)),CHECK(NOT removed OR issue_id IS NOT NULL)
);
ALTER TABLE attachment_uploads ADD CONSTRAINT fk_upload_finalized_attachment FOREIGN KEY(workspace_id,attachment_id,id) REFERENCES attachments(workspace_id,id,upload_id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX idx_attachment_issue_page ON attachments(workspace_id,issue_id,uuid);
CREATE FUNCTION protect_attachment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'attachment identity is permanent';END IF;
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.upload_id,NEW.actor_id,NEW.draft_uuid,NEW.file_revision,NEW.filename,NEW.content_type,NEW.size_bytes,NEW.sha256,NEW.manifest_key,NEW.finalized_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.upload_id,OLD.actor_id,OLD.draft_uuid,OLD.file_revision,OLD.filename,OLD.content_type,OLD.size_bytes,OLD.sha256,OLD.manifest_key,OLD.finalized_at) THEN RAISE EXCEPTION 'file bytes and metadata are immutable';END IF;
 IF OLD.issue_id IS NOT NULL AND NEW.issue_id IS DISTINCT FROM OLD.issue_id THEN RAISE EXCEPTION 'single issue ownership is permanent';END IF;
 IF OLD.removed AND NOT NEW.removed THEN RAISE EXCEPTION 'removed attachment cannot be rebound';END IF;
 IF NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'attachment version must advance';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_attachment_identity BEFORE UPDATE OR DELETE ON attachments FOR EACH ROW EXECUTE FUNCTION protect_attachment();
CREATE TABLE attachment_chunks (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,workspace_id BIGINT NOT NULL,upload_id BIGINT NOT NULL,
 offset_bytes BIGINT NOT NULL CHECK(offset_bytes>=0),size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 262144),sha256 TEXT NOT NULL CHECK(sha256~'^[a-f0-9]{64}$'),object_key TEXT NOT NULL UNIQUE,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,upload_id) REFERENCES attachment_uploads(workspace_id,id),UNIQUE(workspace_id,upload_id,offset_bytes),UNIQUE(workspace_id,id)
);
CREATE FUNCTION validate_attachment_chunk() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE u attachment_uploads;BEGIN
 SELECT * INTO STRICT u FROM attachment_uploads WHERE workspace_id=NEW.workspace_id AND id=NEW.upload_id;
 IF NEW.offset_bytes%u.chunk_bytes<>0 OR NEW.offset_bytes>=u.size_bytes OR NEW.size_bytes<>LEAST(u.chunk_bytes::bigint,u.size_bytes-NEW.offset_bytes) THEN RAISE EXCEPTION 'invalid chunk geometry' USING ERRCODE='23514';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_attachment_chunk_shape BEFORE INSERT ON attachment_chunks FOR EACH ROW EXECUTE FUNCTION validate_attachment_chunk();
CREATE TRIGGER trg_attachment_chunk_immutable BEFORE UPDATE OR DELETE ON attachment_chunks FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE TABLE attachment_file_chunks (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,workspace_id BIGINT NOT NULL,attachment_id BIGINT NOT NULL,upload_chunk_id BIGINT NOT NULL,object_key TEXT NOT NULL UNIQUE,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,attachment_id) REFERENCES attachments(workspace_id,id),FOREIGN KEY(workspace_id,upload_chunk_id) REFERENCES attachment_chunks(workspace_id,id),UNIQUE(workspace_id,attachment_id,upload_chunk_id)
);
CREATE FUNCTION validate_attachment_file_chunk() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM attachments f JOIN attachment_chunks c ON c.workspace_id=f.workspace_id AND c.upload_id=f.upload_id WHERE f.workspace_id=NEW.workspace_id AND f.id=NEW.attachment_id AND c.id=NEW.upload_chunk_id) THEN RAISE EXCEPTION 'file chunk belongs to a different upload' USING ERRCODE='23514';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_attachment_file_chunk_shape BEFORE INSERT ON attachment_file_chunks FOR EACH ROW EXECUTE FUNCTION validate_attachment_file_chunk();
CREATE TRIGGER trg_attachment_file_chunk_immutable BEFORE UPDATE OR DELETE ON attachment_file_chunks FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE FUNCTION validate_attachment_upload_link() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM attachment_uploads u WHERE u.workspace_id=NEW.workspace_id AND u.id=NEW.upload_id AND u.attachment_id=NEW.id) THEN RAISE EXCEPTION 'attachment publication requires upload link' USING ERRCODE='23514';END IF;RETURN NEW;END $$;
CREATE CONSTRAINT TRIGGER trg_attachment_upload_link AFTER INSERT ON attachments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_attachment_upload_link();
CREATE TABLE attachment_upload_history (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL UNIQUE,event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),workspace_id BIGINT NOT NULL,upload_id BIGINT NOT NULL,
 actor_uuid UUID NOT NULL REFERENCES users(uuid),event_name TEXT NOT NULL,version BIGINT NOT NULL,occurred_at TIMESTAMPTZ NOT NULL,snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 FOREIGN KEY(workspace_id,upload_id) REFERENCES attachment_uploads(workspace_id,id),UNIQUE(workspace_id,upload_id,version)
);
CREATE INDEX idx_attachment_upload_history_page ON attachment_upload_history(workspace_id,upload_id,uuid);
CREATE TRIGGER trg_attachment_upload_history_immutable BEFORE UPDATE OR DELETE ON attachment_upload_history FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE TABLE attachment_history (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL UNIQUE,event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),workspace_id BIGINT NOT NULL,attachment_id BIGINT NOT NULL,
 actor_uuid UUID NOT NULL REFERENCES users(uuid),event_name TEXT NOT NULL,version BIGINT NOT NULL,occurred_at TIMESTAMPTZ NOT NULL,snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 FOREIGN KEY(workspace_id,attachment_id) REFERENCES attachments(workspace_id,id),UNIQUE(workspace_id,attachment_id,version)
);
CREATE INDEX idx_attachment_history_page ON attachment_history(workspace_id,attachment_id,uuid);
CREATE TRIGGER trg_attachment_history_immutable BEFORE UPDATE OR DELETE ON attachment_history FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE VIEW attachment_upload_public AS SELECT u.*,w.uuid AS workspace_uuid,a.uuid AS actor_uuid,f.uuid AS attachment_uuid FROM attachment_uploads u JOIN workspaces w ON w.id=u.workspace_id JOIN users a ON a.id=u.actor_id LEFT JOIN attachments f ON f.workspace_id=u.workspace_id AND f.id=u.attachment_id;
CREATE VIEW attachment_public AS SELECT f.*,w.uuid AS workspace_uuid,u.uuid AS upload_uuid,a.uuid AS actor_uuid,i.uuid AS issue_uuid,up.state AS upload_state,up.expires_at AS upload_expires_at,t.uuid AS team_uuid,p.uuid AS project_uuid,COALESCE((t.code||'-'||i.number)::text,'') AS issue_identifier,NOT i.archived AND NOT t.archived AS issue_active FROM attachments f JOIN workspaces w ON w.id=f.workspace_id JOIN attachment_uploads u ON u.workspace_id=f.workspace_id AND u.id=f.upload_id JOIN attachment_uploads up ON up.id=u.id JOIN users a ON a.id=f.actor_id LEFT JOIN issues i ON i.workspace_id=f.workspace_id AND i.id=f.issue_id LEFT JOIN teams t ON t.workspace_id=i.workspace_id AND t.id=i.team_id LEFT JOIN projects p ON p.workspace_id=i.workspace_id AND p.id=i.project_id;
UPDATE event_consumers SET event_names=event_names||ARRAY['attachment_uploads.created','attachment_uploads.chunk_uploaded','attachment_uploads.finalized','attachment_uploads.bound','attachment_uploads.removed','attachment_uploads.aborted','attachment_uploads.expired','attachments.finalized','attachments.bound','attachments.removed'] WHERE name='workspace.activity' AND version=1;
