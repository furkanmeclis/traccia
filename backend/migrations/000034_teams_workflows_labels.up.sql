CREATE TABLE teams (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100),
 code TEXT NOT NULL CHECK(code ~ '^[A-Z][A-Z0-9]{1,9}$'),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), workflow_version BIGINT NOT NULL DEFAULT 1 CHECK(workflow_version>0),
 next_issue_number BIGINT NOT NULL DEFAULT 1 CHECK(next_issue_number>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,code), UNIQUE(workspace_id,id), UNIQUE(workspace_id,uuid)
);
CREATE INDEX idx_teams_workspace ON teams(workspace_id,uuid);
CREATE TRIGGER trg_teams_updated_at BEFORE UPDATE ON teams FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE workflow_statuses (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL, team_id BIGINT NOT NULL,
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100),
 category TEXT NOT NULL CHECK(category IN ('backlog','unstarted','started','completed','canceled')),
 color TEXT NOT NULL CHECK(color ~ '^#[0-9A-Fa-f]{6}$'), position BIGINT NOT NULL CHECK(position>=0),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 FOREIGN KEY(workspace_id,team_id) REFERENCES teams(workspace_id,id), UNIQUE(workspace_id,team_id,id),
 UNIQUE(team_id,position) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX idx_workflow_statuses_team ON workflow_statuses(workspace_id,team_id,position);
CREATE TABLE labels (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id), team_id BIGINT,
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100), color TEXT NOT NULL CHECK(color ~ '^#[0-9A-Fa-f]{6}$'),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 FOREIGN KEY(workspace_id,team_id) REFERENCES teams(workspace_id,id), UNIQUE(workspace_id,id)
);
CREATE UNIQUE INDEX uq_labels_shared_name ON labels(workspace_id,lower(name)) WHERE team_id IS NULL AND NOT archived;
CREATE UNIQUE INDEX uq_labels_team_name ON labels(workspace_id,team_id,lower(name)) WHERE team_id IS NOT NULL AND NOT archived;
CREATE INDEX idx_labels_workspace ON labels(workspace_id,team_id,uuid);
ALTER TABLE workspace_history ADD COLUMN resource_type TEXT NOT NULL DEFAULT 'workspace', ADD COLUMN resource_uuid UUID, ADD COLUMN team_uuid UUID;
UPDATE event_consumers SET event_names=event_names || ARRAY['teams.created','teams.updated','teams.archived','workflow_statuses.created','workflow_statuses.updated','workflow_statuses.archived','workflow_statuses.reordered','labels.created','labels.updated','labels.archived'] WHERE name='workspace.activity' AND version=1;

CREATE FUNCTION protect_team_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.uuid,NEW.workspace_id,NEW.code) IS DISTINCT FROM ROW(OLD.uuid,OLD.workspace_id,OLD.code) THEN
  RAISE EXCEPTION 'team identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_team_identity BEFORE UPDATE ON teams FOR EACH ROW EXECUTE FUNCTION protect_team_identity();
