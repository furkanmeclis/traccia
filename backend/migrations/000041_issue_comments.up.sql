CREATE TABLE issue_comments (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),issue_id BIGINT NOT NULL,author_uuid UUID NOT NULL REFERENCES users(uuid),created_channel TEXT NOT NULL CHECK(created_channel IN ('api','mcp','panel','unknown')),deleted BOOLEAN NOT NULL DEFAULT FALSE,
 format TEXT NOT NULL DEFAULT 'plate' CHECK(format='plate'),schema_version INTEGER NOT NULL DEFAULT 1 CHECK(schema_version=1),
 head_revision UUID NOT NULL,version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id)
);
CREATE INDEX idx_comment_issue ON issue_comments(workspace_id,issue_id,id);
CREATE TRIGGER trg_comment_updated_at BEFORE UPDATE ON issue_comments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_comment_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.issue_id,NEW.author_uuid,NEW.created_channel,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.issue_id,OLD.author_uuid,OLD.created_channel,OLD.created_at) THEN RAISE EXCEPTION 'document identity is immutable'; END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_comment_identity BEFORE UPDATE ON issue_comments FOR EACH ROW EXECUTE FUNCTION protect_comment_identity();
CREATE TABLE comment_revisions (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL UNIQUE,workspace_id BIGINT NOT NULL,comment_id BIGINT NOT NULL,
 parent_revision UUID,source_revision UUID,version BIGINT NOT NULL CHECK(version>0),schema_version INTEGER NOT NULL CHECK(schema_version=1),
 kind TEXT NOT NULL CHECK(kind IN ('create','replace','patch','delete','restore')),checkpoint JSONB,patches JSONB NOT NULL,
 channel TEXT NOT NULL CHECK(channel IN ('api','mcp','panel','unknown')),deleted BOOLEAN NOT NULL,actor_uuid UUID NOT NULL REFERENCES users(uuid),event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),occurred_at TIMESTAMPTZ NOT NULL,
 FOREIGN KEY(workspace_id,comment_id) REFERENCES issue_comments(workspace_id,id),
 UNIQUE(workspace_id,comment_id,uuid),UNIQUE(workspace_id,comment_id,version),
 FOREIGN KEY(workspace_id,comment_id,parent_revision) REFERENCES comment_revisions(workspace_id,comment_id,uuid),
 FOREIGN KEY(workspace_id,comment_id,source_revision) REFERENCES comment_revisions(workspace_id,comment_id,uuid),
 CHECK((kind='create' AND parent_revision IS NULL AND version=1) OR (kind<>'create' AND parent_revision IS NOT NULL AND version>1)),
 CHECK((kind='restore')=(source_revision IS NOT NULL)),
 CHECK(jsonb_typeof(patches)='array'),
 CHECK((kind='patch' AND checkpoint IS NULL AND jsonb_array_length(patches)>0) OR (kind<>'patch' AND checkpoint IS NOT NULL AND jsonb_typeof(checkpoint)='array' AND jsonb_array_length(checkpoint)>0 AND jsonb_array_length(patches)=0))
);
ALTER TABLE issue_comments ADD CONSTRAINT fk_comment_head FOREIGN KEY(workspace_id,id,head_revision) REFERENCES comment_revisions(workspace_id,comment_id,uuid) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX idx_comment_revision_page ON comment_revisions(workspace_id,comment_id,id);
CREATE TRIGGER trg_comment_revision_immutable BEFORE UPDATE OR DELETE ON comment_revisions FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE TABLE comment_references (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL,comment_id BIGINT NOT NULL,node_uuid UUID NOT NULL,revision_uuid UUID NOT NULL,
 user_id BIGINT,issue_id BIGINT,project_id BIGINT,value TEXT NOT NULL,active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(num_nonnulls(user_id,issue_id,project_id)=1),
 FOREIGN KEY(workspace_id,comment_id) REFERENCES issue_comments(workspace_id,id),
 FOREIGN KEY(workspace_id,comment_id,revision_uuid) REFERENCES comment_revisions(workspace_id,comment_id,uuid),
 FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE UNIQUE INDEX idx_comment_reference_node ON comment_references(workspace_id,comment_id,node_uuid) WHERE active;
CREATE INDEX idx_comment_reference_user ON comment_references(workspace_id,user_id,uuid);
CREATE INDEX idx_comment_reference_issue ON comment_references(workspace_id,issue_id,uuid);
CREATE INDEX idx_comment_reference_project ON comment_references(workspace_id,project_id,uuid);
CREATE TRIGGER trg_comment_reference_updated_at BEFORE UPDATE ON comment_references FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_comment_reference_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'document reference anchors are immutable';END IF;
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.comment_id,NEW.node_uuid,NEW.user_id,NEW.issue_id,NEW.project_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.comment_id,OLD.node_uuid,OLD.user_id,OLD.issue_id,OLD.project_id,OLD.created_at) OR NOT OLD.active THEN RAISE EXCEPTION 'document reference identity is immutable';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_comment_reference_identity BEFORE UPDATE OR DELETE ON comment_references FOR EACH ROW EXECUTE FUNCTION protect_comment_reference_identity();

CREATE VIEW comment_reference_public AS
 SELECT r.*,w.uuid AS workspace_uuid,c.uuid AS comment_uuid,oi.uuid AS owner_uuid,
 CASE WHEN r.user_id IS NOT NULL THEN 'user' WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE 'project' END::text AS target_kind,COALESCE(u.uuid,i.uuid,p.uuid) AS target_uuid
 FROM comment_references r JOIN workspaces w ON w.id=r.workspace_id JOIN issue_comments c ON c.workspace_id=r.workspace_id AND c.id=r.comment_id
 JOIN issues oi ON oi.workspace_id=c.workspace_id AND oi.id=c.issue_id
 LEFT JOIN users u ON u.id=r.user_id LEFT JOIN issues i ON i.workspace_id=r.workspace_id AND i.id=r.issue_id LEFT JOIN projects p ON p.workspace_id=r.workspace_id AND p.id=r.project_id;
ALTER TABLE workspace_history ADD COLUMN source_channel TEXT NOT NULL DEFAULT 'unknown' CHECK(source_channel IN ('api','mcp','panel','unknown'));
ALTER TABLE workspace_history ALTER COLUMN source_channel SET DEFAULT COALESCE(NULLIF(current_setting('traccia.source_channel',true),''),'unknown');
UPDATE event_consumers SET event_names=event_names||ARRAY['comments.created','comments.replaced','comments.patched','comments.deleted','comments.restored'] WHERE name='workspace.activity' AND version=1;

-- Canonical history already binds each relation endpoint separately. Never
-- derive eligibility from mutable names, current attachment ownership or JSON.
CREATE VIEW issue_timeline_events AS
 SELECT h.*,i.id AS owning_issue_id,w.id AS owning_workspace_id
 FROM workspace_history h JOIN workspaces w ON w.uuid=h.workspace_uuid
 JOIN issues i ON i.workspace_id=w.id AND i.uuid=h.issue_uuid;
CREATE INDEX idx_workspace_history_issue_cursor ON workspace_history(workspace_uuid,issue_uuid,id);
