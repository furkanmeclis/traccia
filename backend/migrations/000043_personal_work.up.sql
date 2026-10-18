CREATE TABLE personal_portfolio_sessions (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),actor_uuid UUID NOT NULL REFERENCES users(uuid),request_uuid UUID NOT NULL,
 resource TEXT NOT NULL CHECK(resource IN ('issue','project','team')),fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),query JSONB,
 workspace_uuids UUID[] NOT NULL CHECK(cardinality(workspace_uuids) BETWEEN 1 AND 32),workspace_snapshots JSONB NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(workspace_snapshots)='array'),assigned_to_me BOOLEAN NOT NULL,
 first_limit INTEGER NOT NULL CHECK(first_limit BETWEEN 1 AND 100),state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','ready','expired')),
 total_count BIGINT NOT NULL DEFAULT 0 CHECK(total_count>=0),as_of TIMESTAMPTZ NOT NULL,expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,actor_uuid,request_uuid),UNIQUE(workspace_id,id),CHECK(expires_at>as_of),CHECK((state='expired')=(query IS NULL)),CHECK(query IS NULL OR jsonb_typeof(query)='object')
);
CREATE INDEX idx_personal_portfolio_expiry ON personal_portfolio_sessions(expires_at,id);
-- Session headers are small permanent request-id tombstones after item cleanup.
CREATE FUNCTION protect_personal_portfolio_session() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'query request tombstones are retained';END IF;
 IF OLD.state='ready' AND NEW.state='expired' THEN
  IF OLD.expires_at>clock_timestamp() OR NEW.query IS NOT NULL OR EXISTS(SELECT 1 FROM personal_portfolio_items WHERE session_id=OLD.id) OR ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.actor_uuid,NEW.request_uuid,NEW.resource,NEW.fingerprint,NEW.first_limit,NEW.workspace_uuids,NEW.workspace_snapshots,NEW.assigned_to_me,NEW.total_count,NEW.as_of,NEW.expires_at,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.actor_uuid,OLD.request_uuid,OLD.resource,OLD.fingerprint,OLD.first_limit,OLD.workspace_uuids,OLD.workspace_snapshots,OLD.assigned_to_me,OLD.total_count,OLD.as_of,OLD.expires_at,OLD.created_at) THEN RAISE EXCEPTION 'invalid query tombstone compaction';END IF;RETURN NEW;
 END IF;
 IF OLD.state<>'pending' OR NEW.state<>'ready' OR ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.actor_uuid,NEW.request_uuid,NEW.resource,NEW.fingerprint,NEW.query,NEW.first_limit,NEW.workspace_uuids,NEW.assigned_to_me,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.actor_uuid,OLD.request_uuid,OLD.resource,OLD.fingerprint,OLD.query,OLD.first_limit,OLD.workspace_uuids,OLD.assigned_to_me,OLD.created_at) THEN RAISE EXCEPTION 'query snapshot is immutable';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_personal_portfolio_identity BEFORE UPDATE OR DELETE ON personal_portfolio_sessions FOR EACH ROW EXECUTE FUNCTION protect_personal_portfolio_session();
CREATE TABLE personal_portfolio_items (
 id BIGSERIAL PRIMARY KEY,workspace_id BIGINT NOT NULL,session_id BIGINT NOT NULL,ordinal BIGINT NOT NULL CHECK(ordinal>0),source_workspace_uuid UUID NOT NULL REFERENCES workspaces(uuid),resource_uuid UUID NOT NULL,snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 FOREIGN KEY(workspace_id,session_id) REFERENCES personal_portfolio_sessions(workspace_id,id),UNIQUE(session_id,ordinal),UNIQUE(session_id,source_workspace_uuid,resource_uuid)
);
CREATE FUNCTION protect_personal_portfolio_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='INSERT' THEN IF NOT EXISTS(SELECT 1 FROM personal_portfolio_sessions WHERE id=NEW.session_id AND workspace_id=NEW.workspace_id AND state='pending' AND NEW.source_workspace_uuid=ANY(workspace_uuids)) THEN RAISE EXCEPTION 'query snapshot is already published';END IF;RETURN NEW;END IF;
 IF TG_OP='UPDATE' OR EXISTS(SELECT 1 FROM personal_portfolio_sessions WHERE id=OLD.session_id AND expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'query snapshot is immutable';END IF;RETURN OLD;END $$;
CREATE TRIGGER trg_personal_portfolio_item_identity BEFORE INSERT OR UPDATE OR DELETE ON personal_portfolio_items FOR EACH ROW EXECUTE FUNCTION protect_personal_portfolio_item();

CREATE TABLE personal_favorites (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),actor_uuid UUID NOT NULL REFERENCES users(uuid),
 kind TEXT NOT NULL CHECK(kind IN ('issue','project','team','workspace')),target_uuid UUID NOT NULL,
 issue_uuid UUID,project_uuid UUID,team_uuid UUID,revision UUID NOT NULL,active BOOLEAN NOT NULL,
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,actor_uuid,kind,target_uuid),UNIQUE(workspace_id,actor_uuid,id),
 FOREIGN KEY(workspace_id,issue_uuid) REFERENCES issues(workspace_id,uuid),
 FOREIGN KEY(workspace_id,project_uuid) REFERENCES projects(workspace_id,uuid),
 FOREIGN KEY(workspace_id,team_uuid) REFERENCES teams(workspace_id,uuid),
 CHECK((kind='issue' AND issue_uuid=target_uuid AND project_uuid IS NULL AND team_uuid IS NULL) OR (kind='project' AND project_uuid=target_uuid AND issue_uuid IS NULL AND team_uuid IS NULL) OR (kind='team' AND team_uuid=target_uuid AND issue_uuid IS NULL AND project_uuid IS NULL) OR (kind='workspace' AND num_nonnulls(issue_uuid,project_uuid,team_uuid)=0)),
 CHECK((kind='issue')=(issue_uuid IS NOT NULL) AND (kind='project')=(project_uuid IS NOT NULL) AND (kind='team')=(team_uuid IS NOT NULL))
);
CREATE INDEX idx_personal_favorites_actor ON personal_favorites(workspace_id,actor_uuid,uuid);
CREATE FUNCTION protect_personal_favorite() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'favorite identities retained';END IF;
 IF NEW.kind='workspace' AND NOT EXISTS(SELECT 1 FROM workspaces WHERE id=NEW.workspace_id AND uuid=NEW.target_uuid) THEN RAISE EXCEPTION 'favorite workspace mismatch';END IF;
 IF TG_OP='UPDATE' AND (ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.actor_uuid,NEW.kind,NEW.target_uuid,NEW.issue_uuid,NEW.project_uuid,NEW.team_uuid,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.actor_uuid,OLD.kind,OLD.target_uuid,OLD.issue_uuid,OLD.project_uuid,OLD.team_uuid,OLD.created_at) OR NEW.version<>OLD.version+1 OR NEW.revision=OLD.revision) THEN RAISE EXCEPTION 'favorite identity or revision invalid';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_personal_favorite_identity BEFORE INSERT OR UPDATE OR DELETE ON personal_favorites FOR EACH ROW EXECUTE FUNCTION protect_personal_favorite();
CREATE TRIGGER trg_personal_favorite_updated_at BEFORE UPDATE ON personal_favorites FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE personal_favorite_history (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,workspace_id BIGINT NOT NULL,actor_uuid UUID NOT NULL,favorite_id BIGINT NOT NULL,
 revision UUID NOT NULL,version BIGINT NOT NULL CHECK(version>0),snapshot JSONB NOT NULL,event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),occurred_at TIMESTAMPTZ NOT NULL,
 FOREIGN KEY(workspace_id,actor_uuid,favorite_id) REFERENCES personal_favorites(workspace_id,actor_uuid,id),UNIQUE(workspace_id,revision)
);
CREATE TRIGGER trg_personal_favorite_history BEFORE UPDATE OR DELETE ON personal_favorite_history FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
UPDATE event_consumers SET event_names=event_names||ARRAY['favorites.set'] WHERE name='workspace.activity' AND version=1;

CREATE VIEW personal_favorite_targets AS
 SELECT 'issue'::text AS kind,w.id AS workspace_id,w.uuid AS workspace_uuid,i.uuid AS target_uuid,i.document->>'title' AS label,i.document->>'identifier' AS identifier,i.archived FROM issue_public i JOIN workspaces w ON w.uuid=i.workspace_uuid
 UNION ALL SELECT 'project',w.id,w.uuid,p.uuid,p.name,NULL::text,p.archived FROM projects p JOIN workspaces w ON w.id=p.workspace_id
 UNION ALL SELECT 'team',w.id,w.uuid,t.uuid,t.name,t.code,t.archived FROM teams t JOIN workspaces w ON w.id=t.workspace_id
 UNION ALL SELECT 'workspace',w.id,w.uuid,w.uuid,w.name,NULL::text,FALSE FROM workspaces w;
