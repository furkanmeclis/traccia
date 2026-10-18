CREATE TABLE issues (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id), team_id BIGINT NOT NULL,
 number BIGINT NOT NULL CHECK(number>0), status_id BIGINT NOT NULL,
 title TEXT NOT NULL CHECK(char_length(title) BETWEEN 1 AND 255 AND title=btrim(title) AND title !~ '[[:cntrl:]]' AND position(chr(8232) in title)=0 AND position(chr(8233) in title)=0),
 priority TEXT NOT NULL CHECK(priority IN ('none','urgent','high','normal','low')),
 assignee_id BIGINT, project_id BIGINT, milestone_id BIGINT, start_date DATE, target_date DATE,
 effort INTEGER CHECK(effort BETWEEN 0 AND 1000),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), UNIQUE(workspace_id,uuid), UNIQUE(workspace_id,team_id,number),
 FOREIGN KEY(workspace_id,team_id) REFERENCES teams(workspace_id,id),
 FOREIGN KEY(workspace_id,team_id,status_id) REFERENCES workflow_statuses(workspace_id,team_id,id),
 FOREIGN KEY(workspace_id,assignee_id) REFERENCES workspace_members(workspace_id,user_id),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
 FOREIGN KEY(workspace_id,project_id,milestone_id) REFERENCES project_milestones(workspace_id,project_id,id),
 CHECK(milestone_id IS NULL OR project_id IS NOT NULL), CHECK(start_date IS NULL OR target_date IS NULL OR start_date<=target_date)
);
CREATE INDEX idx_issues_page ON issues(workspace_id,archived,uuid);
CREATE TRIGGER trg_issues_updated_at BEFORE UPDATE ON issues FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE issue_aliases (
 workspace_id BIGINT NOT NULL, team_id BIGINT NOT NULL, number BIGINT NOT NULL CHECK(number>0), issue_id BIGINT NOT NULL,
 PRIMARY KEY(workspace_id,team_id,number),
 FOREIGN KEY(workspace_id,team_id) REFERENCES teams(workspace_id,id),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id)
);
CREATE TABLE issue_labels (
 workspace_id BIGINT NOT NULL, issue_id BIGINT NOT NULL, label_id BIGINT NOT NULL,
 PRIMARY KEY(workspace_id,issue_id,label_id),
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id),
 FOREIGN KEY(workspace_id,label_id) REFERENCES labels(workspace_id,id)
);
CREATE FUNCTION protect_issue_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF ROW(NEW.uuid,NEW.workspace_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.uuid,OLD.workspace_id,OLD.created_at) THEN RAISE EXCEPTION 'issue identity is immutable'; END IF; RETURN NEW; END $$;
CREATE TRIGGER trg_issue_identity BEFORE UPDATE ON issues FOR EACH ROW EXECUTE FUNCTION protect_issue_identity();
CREATE FUNCTION protect_issue_alias() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'issue aliases are immutable'; END $$;
CREATE TRIGGER trg_issue_alias_immutable BEFORE UPDATE OR DELETE ON issue_aliases FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE TABLE issue_history (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL UNIQUE, event_id BIGINT NOT NULL UNIQUE REFERENCES outbox_events(id),
 workspace_id BIGINT NOT NULL, issue_id BIGINT NOT NULL, actor_uuid UUID NOT NULL REFERENCES users(uuid),
 event_name TEXT NOT NULL, version BIGINT NOT NULL, occurred_at TIMESTAMPTZ NOT NULL, snapshot JSONB NOT NULL,
 FOREIGN KEY(workspace_id,issue_id) REFERENCES issues(workspace_id,id), UNIQUE(workspace_id,issue_id,version)
);
CREATE INDEX idx_issue_history_page ON issue_history(workspace_id,issue_id,id);
CREATE TRIGGER trg_issue_history_immutable BEFORE UPDATE OR DELETE ON issue_history FOR EACH ROW EXECUTE FUNCTION protect_issue_alias();
CREATE VIEW issue_public AS SELECT i.*,w.uuid AS workspace_uuid,t.uuid AS team_uuid,
 jsonb_build_object('id',i.uuid,'workspace_id',w.uuid,'team_id',t.uuid,'identifier',t.code||'-'||i.number,'number',i.number,
 'status_id',s.uuid,'title',i.title,'priority',i.priority,'assignee_id',u.uuid,'project_id',p.uuid,'milestone_id',m.uuid,
 'start_date',i.start_date,'target_date',i.target_date,'effort',i.effort,'version',i.version,'archived',i.archived,
 'created_at',i.created_at,'updated_at',i.updated_at,'label_ids',COALESCE((SELECT jsonb_agg(l.uuid ORDER BY l.uuid) FROM issue_labels il JOIN labels l ON l.id=il.label_id AND l.workspace_id=il.workspace_id WHERE il.issue_id=i.id AND il.workspace_id=i.workspace_id),'[]'::jsonb)) AS document
 FROM issues i JOIN workspaces w ON w.id=i.workspace_id JOIN teams t ON t.id=i.team_id JOIN workflow_statuses s ON s.id=i.status_id
 LEFT JOIN users u ON u.id=i.assignee_id LEFT JOIN projects p ON p.id=i.project_id LEFT JOIN project_milestones m ON m.id=i.milestone_id;
UPDATE event_consumers SET event_names=event_names||ARRAY['issues.created','issues.updated','issues.moved','issues.archived','issues.restored'] WHERE name='workspace.activity' AND version=1;

ALTER TABLE workspace_history ADD COLUMN issue_uuid UUID, ADD COLUMN issue_identifier TEXT NOT NULL DEFAULT '';
