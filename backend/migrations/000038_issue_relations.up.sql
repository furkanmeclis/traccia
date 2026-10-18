CREATE TABLE issue_relation_revisions (
 workspace_id BIGINT NOT NULL, issue_id BIGINT NOT NULL, version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 PRIMARY KEY(workspace_id,issue_id),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id)
);
CREATE TABLE issue_relations (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL, source_issue_id BIGINT NOT NULL, target_issue_id BIGINT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('parent','blocks','duplicate','related')),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version IN (1,2)),
 removed BOOLEAN NOT NULL DEFAULT FALSE, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), removed_at TIMESTAMPTZ,
 UNIQUE(workspace_id,id),
 FOREIGN KEY(workspace_id,source_issue_id) REFERENCES issues(workspace_id,id),
 FOREIGN KEY(workspace_id,target_issue_id) REFERENCES issues(workspace_id,id),
 CHECK(source_issue_id<>target_issue_id), CHECK(kind<>'related' OR source_issue_id<target_issue_id),
 CHECK((NOT removed AND version=1 AND removed_at IS NULL) OR (removed AND version=2 AND removed_at IS NOT NULL))
);
CREATE UNIQUE INDEX idx_issue_relations_active_pair ON issue_relations(workspace_id,source_issue_id,target_issue_id,kind) WHERE NOT removed;
CREATE UNIQUE INDEX idx_issue_relations_single_target ON issue_relations(workspace_id,source_issue_id,kind) WHERE NOT removed AND kind IN ('parent','duplicate');
CREATE INDEX idx_issue_relations_source ON issue_relations(workspace_id,source_issue_id,uuid);
CREATE INDEX idx_issue_relations_target ON issue_relations(workspace_id,target_issue_id,uuid);
CREATE FUNCTION protect_issue_relation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'issue relation identities are immutable'; END IF;
 IF ROW(NEW.id,NEW.uuid,NEW.workspace_id,NEW.source_issue_id,NEW.target_issue_id,NEW.kind,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.uuid,OLD.workspace_id,OLD.source_issue_id,OLD.target_issue_id,OLD.kind,OLD.created_at) OR OLD.removed OR NOT NEW.removed THEN RAISE EXCEPTION 'only removal of an active relation is allowed'; END IF;
 RETURN NEW; END $$;
CREATE TRIGGER trg_issue_relation_identity BEFORE UPDATE OR DELETE ON issue_relations FOR EACH ROW EXECUTE FUNCTION protect_issue_relation();
CREATE TABLE issue_relation_history (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL UNIQUE, event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),
 workspace_id BIGINT NOT NULL, issue_id BIGINT NOT NULL, actor_uuid UUID NOT NULL REFERENCES users(uuid),
 event_name TEXT NOT NULL, version BIGINT NOT NULL CHECK(version>1), occurred_at TIMESTAMPTZ NOT NULL, snapshot JSONB NOT NULL,
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id), UNIQUE(workspace_id,issue_id,version)
);
CREATE INDEX idx_issue_relation_history_page ON issue_relation_history(workspace_id,issue_id,id);
CREATE TRIGGER trg_issue_relation_history_immutable BEFORE UPDATE OR DELETE ON issue_relation_history FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE VIEW relation_issue_reference AS SELECT i.id,i.workspace_id,i.team_id,i.uuid,w.uuid AS workspace_uuid,
 jsonb_build_object('id',i.uuid,'workspace_id',w.uuid,'team_id',t.uuid,'identifier',t.code||'-'||i.number,'title',i.title,'archived',i.archived,'active',NOT i.archived AND NOT t.archived,'relation_version',COALESCE(r.version,1)) AS document
 FROM issues i JOIN workspaces w ON w.id=i.workspace_id JOIN teams t ON t.id=i.team_id
 LEFT JOIN issue_relation_revisions r ON r.workspace_id=i.workspace_id AND r.issue_id=i.id;
CREATE VIEW relation_public AS SELECT r.*,w.uuid AS workspace_uuid,s.uuid AS source_uuid,t.uuid AS target_uuid,
 jsonb_build_object('id',r.uuid,'workspace_id',w.uuid,'kind',r.kind,'source',s.document,'target',t.document,'version',r.version,'removed',r.removed,'created_at',r.created_at,'removed_at',r.removed_at) AS document
 FROM issue_relations r JOIN workspaces w ON w.id=r.workspace_id
 JOIN relation_issue_reference s ON s.workspace_id=r.workspace_id AND s.id=r.source_issue_id
 JOIN relation_issue_reference t ON t.workspace_id=r.workspace_id AND t.id=r.target_issue_id;
CREATE FUNCTION relation_state(w UUID,i UUID) RETURNS JSONB LANGUAGE sql STABLE AS $$
 WITH RECURSIVE chain AS (
  SELECT r.target_issue_id,ARRAY[r.source_issue_id,r.target_issue_id] AS path FROM issue_relations r JOIN relation_issue_reference s ON s.workspace_id=r.workspace_id AND s.id=r.source_issue_id WHERE s.workspace_uuid=w AND s.uuid=i AND r.kind='duplicate' AND NOT r.removed
  UNION ALL SELECT r.target_issue_id,c.path||r.target_issue_id FROM chain c JOIN issue_relations r ON r.source_issue_id=c.target_issue_id JOIN workspaces ws ON ws.id=r.workspace_id WHERE ws.uuid=w AND r.kind='duplicate' AND NOT r.removed AND NOT r.target_issue_id=ANY(c.path)
 )
 SELECT jsonb_build_object('issue',s.document,'parent',(SELECT t.document FROM relation_public r JOIN relation_issue_reference t ON t.workspace_id=r.workspace_id AND t.id=r.target_issue_id WHERE r.workspace_uuid=w AND r.source_uuid=i AND r.kind='parent' AND NOT r.removed),'duplicate_of',(SELECT t.document FROM relation_public r JOIN relation_issue_reference t ON t.workspace_id=r.workspace_id AND t.id=r.target_issue_id WHERE r.workspace_uuid=w AND r.source_uuid=i AND r.kind='duplicate' AND NOT r.removed),'canonical_duplicate',(SELECT t.document FROM chain c JOIN relation_issue_reference t ON t.workspace_id=s.workspace_id AND t.id=c.target_issue_id ORDER BY cardinality(c.path) DESC LIMIT 1),'capabilities',jsonb_build_object('link',false,'unlink',false)) FROM relation_issue_reference s WHERE s.workspace_uuid=w AND s.uuid=i
$$;
UPDATE event_consumers SET event_names=event_names||ARRAY['issue_relations.parent_set','issue_relations.duplicate_set','issue_relations.block_linked','issue_relations.block_unlinked','issue_relations.related_linked','issue_relations.related_unlinked'] WHERE name='workspace.activity' AND version=1;
