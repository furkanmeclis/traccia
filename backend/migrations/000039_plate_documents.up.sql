CREATE TABLE documents (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),issue_id BIGINT,project_id BIGINT,
 format TEXT NOT NULL DEFAULT 'plate' CHECK(format='plate'),schema_version INTEGER NOT NULL DEFAULT 1 CHECK(schema_version=1),
 head_revision UUID NOT NULL,version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(num_nonnulls(issue_id,project_id)=1),UNIQUE(workspace_id,id),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE UNIQUE INDEX idx_document_issue ON documents(workspace_id,issue_id) WHERE issue_id IS NOT NULL;
CREATE UNIQUE INDEX idx_document_project ON documents(workspace_id,project_id) WHERE project_id IS NOT NULL;
CREATE TRIGGER trg_document_updated_at BEFORE UPDATE ON documents FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_document_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.issue_id,NEW.project_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.issue_id,OLD.project_id,OLD.created_at) THEN RAISE EXCEPTION 'document identity is immutable'; END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_document_identity BEFORE UPDATE ON documents FOR EACH ROW EXECUTE FUNCTION protect_document_identity();
CREATE TABLE document_revisions (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL UNIQUE,workspace_id BIGINT NOT NULL,document_id BIGINT NOT NULL,
 parent_revision UUID,source_revision UUID,version BIGINT NOT NULL CHECK(version>0),schema_version INTEGER NOT NULL CHECK(schema_version=1),
 kind TEXT NOT NULL CHECK(kind IN ('create','replace','patch','restore')),checkpoint JSONB,patches JSONB NOT NULL,
 actor_uuid UUID NOT NULL REFERENCES users(uuid),event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),occurred_at TIMESTAMPTZ NOT NULL,
 FOREIGN KEY(workspace_id,document_id) REFERENCES documents(workspace_id,id),
 UNIQUE(workspace_id,document_id,uuid),UNIQUE(workspace_id,document_id,version),
 FOREIGN KEY(workspace_id,document_id,parent_revision) REFERENCES document_revisions(workspace_id,document_id,uuid),
 FOREIGN KEY(workspace_id,document_id,source_revision) REFERENCES document_revisions(workspace_id,document_id,uuid),
 CHECK((kind='create' AND parent_revision IS NULL AND version=1) OR (kind<>'create' AND parent_revision IS NOT NULL AND version>1)),
 CHECK((kind='restore')=(source_revision IS NOT NULL)),
 CHECK(jsonb_typeof(patches)='array'),
 CHECK((kind='patch' AND checkpoint IS NULL AND jsonb_array_length(patches)>0) OR (kind<>'patch' AND checkpoint IS NOT NULL AND jsonb_typeof(checkpoint)='array' AND jsonb_array_length(checkpoint)>0 AND jsonb_array_length(patches)=0))
);
ALTER TABLE documents ADD CONSTRAINT fk_document_head FOREIGN KEY(workspace_id,id,head_revision) REFERENCES document_revisions(workspace_id,document_id,uuid) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX idx_document_revision_page ON document_revisions(workspace_id,document_id,id);
CREATE TRIGGER trg_document_revision_immutable BEFORE UPDATE OR DELETE ON document_revisions FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE TABLE document_references (
 id BIGSERIAL PRIMARY KEY,uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL,document_id BIGINT NOT NULL,node_uuid UUID NOT NULL,revision_uuid UUID NOT NULL,
 user_id BIGINT,issue_id BIGINT,project_id BIGINT,value TEXT NOT NULL,active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(num_nonnulls(user_id,issue_id,project_id)=1),
 FOREIGN KEY(workspace_id,document_id) REFERENCES documents(workspace_id,id),
 FOREIGN KEY(workspace_id,document_id,revision_uuid) REFERENCES document_revisions(workspace_id,document_id,uuid),
 FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE UNIQUE INDEX idx_document_reference_node ON document_references(workspace_id,document_id,node_uuid) WHERE active;
CREATE INDEX idx_document_reference_user ON document_references(workspace_id,user_id,uuid);
CREATE INDEX idx_document_reference_issue ON document_references(workspace_id,issue_id,uuid);
CREATE INDEX idx_document_reference_project ON document_references(workspace_id,project_id,uuid);
CREATE TRIGGER trg_document_reference_updated_at BEFORE UPDATE ON document_references FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_document_reference_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'document reference anchors are immutable';END IF;
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.document_id,NEW.node_uuid,NEW.user_id,NEW.issue_id,NEW.project_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.document_id,OLD.node_uuid,OLD.user_id,OLD.issue_id,OLD.project_id,OLD.created_at) OR NOT OLD.active THEN RAISE EXCEPTION 'document reference identity is immutable';END IF;RETURN NEW;END $$;
CREATE TRIGGER trg_document_reference_identity BEFORE UPDATE OR DELETE ON document_references FOR EACH ROW EXECUTE FUNCTION protect_document_reference_identity();
CREATE VIEW document_owners AS
 SELECT i.workspace_id,w.uuid AS workspace_uuid,'issue'::text AS kind,i.id,i.uuid,i.title AS name,NOT i.archived AND NOT t.archived AS active,t.uuid AS team_uuid,p.uuid AS project_uuid,(t.code||'-'||i.number)::text AS identifier
 FROM issues i JOIN workspaces w ON w.id=i.workspace_id JOIN teams t ON t.id=i.team_id LEFT JOIN projects p ON p.id=i.project_id AND p.workspace_id=i.workspace_id
 UNION ALL SELECT p.workspace_id,w.uuid,'project'::text,p.id,p.uuid,p.name,NOT p.archived,'00000000-0000-0000-0000-000000000000'::uuid,p.uuid,''::text FROM projects p JOIN workspaces w ON w.id=p.workspace_id;
CREATE VIEW document_reference_public AS
 SELECT r.*,w.uuid AS workspace_uuid,d.uuid AS document_uuid,
 CASE WHEN d.issue_id IS NOT NULL THEN 'issue' ELSE 'project' END::text AS owner_kind,COALESCE(oi.uuid,op.uuid) AS owner_uuid,
 CASE WHEN r.user_id IS NOT NULL THEN 'user' WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE 'project' END::text AS target_kind,COALESCE(u.uuid,i.uuid,p.uuid) AS target_uuid
 FROM document_references r JOIN workspaces w ON w.id=r.workspace_id JOIN documents d ON d.workspace_id=r.workspace_id AND d.id=r.document_id
 LEFT JOIN issues oi ON oi.workspace_id=d.workspace_id AND oi.id=d.issue_id LEFT JOIN projects op ON op.workspace_id=d.workspace_id AND op.id=d.project_id
 LEFT JOIN users u ON u.id=r.user_id LEFT JOIN issues i ON i.workspace_id=r.workspace_id AND i.id=r.issue_id LEFT JOIN projects p ON p.workspace_id=r.workspace_id AND p.id=r.project_id;
UPDATE event_consumers SET event_names=event_names||ARRAY['documents.created','documents.replaced','documents.patched','documents.restored'] WHERE name='workspace.activity' AND version=1;
