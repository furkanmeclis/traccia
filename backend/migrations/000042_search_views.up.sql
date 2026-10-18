CREATE TABLE search_query_sessions (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),actor_uuid UUID NOT NULL REFERENCES users(uuid),request_uuid UUID NOT NULL,
 resource TEXT NOT NULL CHECK(resource IN ('issue','project','team')),fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),query JSONB,
 first_limit INTEGER NOT NULL CHECK(first_limit BETWEEN 1 AND 100),state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','ready','expired')),
 total_count BIGINT NOT NULL DEFAULT 0 CHECK(total_count>=0),as_of TIMESTAMPTZ NOT NULL,expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,actor_uuid,request_uuid),UNIQUE(workspace_id,id),CHECK(expires_at>as_of),CHECK((state='expired')=(query IS NULL)),CHECK(query IS NULL OR jsonb_typeof(query)='object')
);
CREATE INDEX idx_search_session_expiry ON search_query_sessions(expires_at,id);
-- Session headers are small permanent request-id tombstones after item cleanup.
CREATE FUNCTION protect_search_session() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'query request tombstones are retained';END IF;
 IF OLD.state='ready' AND NEW.state='expired' THEN
  IF OLD.expires_at>clock_timestamp() OR NEW.query IS NOT NULL OR EXISTS(SELECT 1 FROM search_query_items WHERE session_id=OLD.id) OR ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.actor_uuid,NEW.request_uuid,NEW.resource,NEW.fingerprint,NEW.first_limit,NEW.total_count,NEW.as_of,NEW.expires_at,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.actor_uuid,OLD.request_uuid,OLD.resource,OLD.fingerprint,OLD.first_limit,OLD.total_count,OLD.as_of,OLD.expires_at,OLD.created_at) THEN RAISE EXCEPTION 'invalid query tombstone compaction';END IF;RETURN NEW;
 END IF;
 IF OLD.state<>'pending' OR NEW.state<>'ready' OR ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.actor_uuid,NEW.request_uuid,NEW.resource,NEW.fingerprint,NEW.query,NEW.first_limit,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.actor_uuid,OLD.request_uuid,OLD.resource,OLD.fingerprint,OLD.query,OLD.first_limit,OLD.created_at) THEN RAISE EXCEPTION 'query snapshot is immutable';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_search_session_identity BEFORE UPDATE OR DELETE ON search_query_sessions FOR EACH ROW EXECUTE FUNCTION protect_search_session();
CREATE TABLE search_query_items (
 id BIGSERIAL PRIMARY KEY,workspace_id BIGINT NOT NULL,session_id BIGINT NOT NULL,ordinal BIGINT NOT NULL CHECK(ordinal>0),resource_uuid UUID NOT NULL,snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 FOREIGN KEY(workspace_id,session_id) REFERENCES search_query_sessions(workspace_id,id),UNIQUE(session_id,ordinal),UNIQUE(session_id,resource_uuid)
);
CREATE FUNCTION protect_search_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='INSERT' THEN IF NOT EXISTS(SELECT 1 FROM search_query_sessions WHERE id=NEW.session_id AND workspace_id=NEW.workspace_id AND state='pending') THEN RAISE EXCEPTION 'query snapshot is already published';END IF;RETURN NEW;END IF;
 IF TG_OP='UPDATE' OR EXISTS(SELECT 1 FROM search_query_sessions WHERE id=OLD.session_id AND expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'query snapshot is immutable';END IF;RETURN OLD;END $$;
CREATE TRIGGER trg_search_item_identity BEFORE INSERT OR UPDATE OR DELETE ON search_query_items FOR EACH ROW EXECUTE FUNCTION protect_search_item();
CREATE TABLE saved_views (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,workspace_id BIGINT NOT NULL REFERENCES workspaces(id),actor_uuid UUID NOT NULL REFERENCES users(uuid),revision UUID NOT NULL,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),resource TEXT NOT NULL CHECK(resource IN ('issue','project','team')),definition JSONB NOT NULL,columns JSONB NOT NULL CHECK(jsonb_typeof(columns)='array'),deleted BOOLEAN NOT NULL DEFAULT FALSE,version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),UNIQUE(workspace_id,id),UNIQUE(workspace_id,actor_uuid,id),CHECK(jsonb_typeof(definition)='object' AND definition->>'resource'=resource)
);
CREATE INDEX idx_saved_view_actor ON saved_views(workspace_id,actor_uuid,id);
CREATE TABLE column_preferences (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,workspace_id BIGINT NOT NULL REFERENCES workspaces(id),actor_uuid UUID NOT NULL REFERENCES users(uuid),resource TEXT NOT NULL CHECK(resource IN ('issue','project','team')),revision UUID NOT NULL,columns JSONB NOT NULL CHECK(jsonb_typeof(columns)='array'),version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),UNIQUE(workspace_id,actor_uuid,resource),UNIQUE(workspace_id,id),UNIQUE(workspace_id,actor_uuid,id)
);
CREATE TRIGGER trg_saved_views_updated_at BEFORE UPDATE ON saved_views FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_column_preferences_updated_at BEFORE UPDATE ON column_preferences FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_private_preference() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'private preference identity is retained';END IF;
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.actor_uuid,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.actor_uuid,OLD.created_at) OR NEW.version<>OLD.version+1 OR NEW.revision=OLD.revision THEN RAISE EXCEPTION 'invalid private preference revision';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_saved_view_identity BEFORE UPDATE OR DELETE ON saved_views FOR EACH ROW EXECUTE FUNCTION protect_private_preference();
CREATE TRIGGER trg_column_preference_identity BEFORE UPDATE OR DELETE ON column_preferences FOR EACH ROW EXECUTE FUNCTION protect_private_preference();
CREATE TABLE private_preference_history (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,workspace_id BIGINT NOT NULL REFERENCES workspaces(id),actor_uuid UUID NOT NULL REFERENCES users(uuid),view_id BIGINT,column_preference_id BIGINT,revision UUID NOT NULL,version BIGINT NOT NULL CHECK(version>0),kind TEXT NOT NULL CHECK(kind IN ('create','update','delete','restore','set')),snapshot JSONB NOT NULL,event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),occurred_at TIMESTAMPTZ NOT NULL,
 CHECK(num_nonnulls(view_id,column_preference_id)=1),FOREIGN KEY(workspace_id,actor_uuid,view_id) REFERENCES saved_views(workspace_id,actor_uuid,id),FOREIGN KEY(workspace_id,actor_uuid,column_preference_id) REFERENCES column_preferences(workspace_id,actor_uuid,id),UNIQUE(workspace_id,revision)
);
CREATE TRIGGER trg_private_preference_history BEFORE UPDATE OR DELETE ON private_preference_history FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();

-- A single source row per entity. Child memberships are aggregated, never joined
-- into the result set, so both predicate evaluation and counts cannot fan out.
CREATE VIEW search_source_rows AS
 SELECT 'issue'::text AS resource,ip.workspace_uuid,ip.uuid,ip.archived,
 ip.document||jsonb_build_object('number',ip.document->>'number','version',ip.document->>'version') AS document FROM issue_public ip
 UNION ALL
 SELECT 'project',w.uuid,p.uuid,p.archived,jsonb_build_object('id',p.uuid,'workspace_id',w.uuid,'name',p.name,'summary',p.summary,'lead_user_id',u.uuid,'team_ids',COALESCE((SELECT jsonb_agg(t.uuid ORDER BY t.uuid) FROM project_teams pt JOIN teams t ON (t.workspace_id,t.id)=(pt.workspace_id,pt.team_id) WHERE pt.workspace_id=p.workspace_id AND pt.project_id=p.id AND NOT pt.archived),'[]'::jsonb),'start_date',p.start_date,'target_date',p.target_date,'version',p.version::text,'archived',p.archived,'created_at',p.created_at,'updated_at',p.updated_at)
 FROM projects p JOIN workspaces w ON w.id=p.workspace_id LEFT JOIN users u ON u.id=p.lead_user_id
 UNION ALL
 SELECT 'team',w.uuid,t.uuid,t.archived,jsonb_build_object('id',t.uuid,'workspace_id',w.uuid,'name',t.name,'code',t.code,'version',t.version::text,'workflow_version',t.workflow_version::text,'archived',t.archived,'created_at',t.created_at,'updated_at',t.updated_at)
 FROM teams t JOIN workspaces w ON w.id=t.workspace_id;
CREATE FUNCTION search_text_fold(value TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE STRICT AS $$ SELECT casefold(value COLLATE pg_catalog.pg_unicode_fast) $$;
CREATE FUNCTION search_filter_matches(f JSONB,doc JSONB) RETURNS BOOLEAN LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE k TEXT;op TEXT;v JSONB;c JSONB;matched BOOLEAN;left_text TEXT;right_text TEXT; BEGIN
 IF f IS NULL OR f='null'::jsonb THEN RETURN TRUE;END IF;k=f->>'kind';op=f->>'op';
 IF k='all' THEN FOR c IN SELECT value FROM jsonb_array_elements(f->'children') LOOP IF NOT search_filter_matches(c,doc) THEN RETURN FALSE;END IF;END LOOP;RETURN TRUE;END IF;
 IF k='any' THEN FOR c IN SELECT value FROM jsonb_array_elements(f->'children') LOOP IF search_filter_matches(c,doc) THEN RETURN TRUE;END IF;END LOOP;RETURN FALSE;END IF;
 IF k='not' THEN RETURN NOT search_filter_matches(f->'child',doc);END IF;
 v=doc->(f->>'field');
 IF k='null' THEN RETURN (v IS NULL OR v='null'::jsonb)=(op='is_null');END IF;
 IF k='uuid' THEN
  IF jsonb_typeof(v)='array' THEN SELECT EXISTS(SELECT 1 FROM jsonb_array_elements_text(v) a(value) WHERE EXISTS(SELECT 1 FROM jsonb_array_elements_text(f->'ids') b(value) WHERE a.value=b.value)) INTO matched;
  ELSE SELECT COALESCE(EXISTS(SELECT 1 FROM jsonb_array_elements_text(f->'ids') b(value) WHERE b.value=v#>>'{}'),FALSE) INTO matched;END IF;
  RETURN CASE WHEN op='not_in' THEN NOT matched ELSE matched END;
 END IF;
 IF k='enum' THEN SELECT EXISTS(SELECT 1 FROM jsonb_array_elements_text(f->'values') b(value) WHERE b.value=v#>>'{}') INTO matched;RETURN CASE WHEN op='not_in' THEN NOT matched ELSE matched END;END IF;
 IF v IS NULL OR v='null'::jsonb THEN RETURN FALSE;END IF;
 IF k='text' THEN left_text=search_text_fold(v#>>'{}');right_text=search_text_fold(f->>'value');RETURN CASE WHEN op='eq' THEN (left_text COLLATE "C")=(right_text COLLATE "C") ELSE strpos(left_text,right_text)>0 END;END IF;
 IF k='number' THEN RETURN CASE op WHEN 'eq' THEN (v#>>'{}')::numeric=(f->>'number')::numeric WHEN 'lt' THEN (v#>>'{}')::numeric<(f->>'number')::numeric WHEN 'lte' THEN (v#>>'{}')::numeric<=(f->>'number')::numeric WHEN 'gt' THEN (v#>>'{}')::numeric>(f->>'number')::numeric ELSE (v#>>'{}')::numeric>=(f->>'number')::numeric END;END IF;
 IF k='date' THEN RETURN CASE op WHEN 'eq' THEN (v#>>'{}')::date=(f->>'value')::date WHEN 'lt' THEN (v#>>'{}')::date<(f->>'value')::date WHEN 'lte' THEN (v#>>'{}')::date<=(f->>'value')::date WHEN 'gt' THEN (v#>>'{}')::date>(f->>'value')::date ELSE (v#>>'{}')::date>=(f->>'value')::date END;END IF;
 RETURN FALSE;END $$;
UPDATE event_consumers SET event_names=event_names||ARRAY['saved_views.created','saved_views.updated','saved_views.deleted','saved_views.restored','column_preferences.set'] WHERE name='workspace.activity' AND version=1;
