CREATE TABLE projects (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id),
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100),
 summary TEXT NOT NULL DEFAULT '' CHECK(char_length(summary)<=2000),
 lead_user_id BIGINT, start_date DATE, target_date DATE,
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), UNIQUE(workspace_id,uuid),
 FOREIGN KEY(workspace_id,lead_user_id) REFERENCES workspace_members(workspace_id,user_id),
 CHECK(start_date IS NULL OR target_date IS NULL OR start_date<=target_date)
);
CREATE INDEX idx_projects_workspace ON projects(workspace_id,archived,uuid);
CREATE TRIGGER trg_projects_updated_at BEFORE UPDATE ON projects FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE project_teams (
 id BIGSERIAL PRIMARY KEY, workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL, team_id BIGINT NOT NULL,
 archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,project_id,team_id),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
 FOREIGN KEY(workspace_id,team_id) REFERENCES teams(workspace_id,id)
);
CREATE TRIGGER trg_project_teams_updated_at BEFORE UPDATE ON project_teams FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE project_milestones (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 name TEXT NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100),
 description TEXT NOT NULL DEFAULT '' CHECK(char_length(description)<=2000), target_date DATE,
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id), UNIQUE(workspace_id,project_id,id)
);
CREATE INDEX idx_project_milestones_page ON project_milestones(workspace_id,project_id,archived,uuid);
CREATE TRIGGER trg_project_milestones_updated_at BEFORE UPDATE ON project_milestones FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE project_resources (
 id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('document','repository','link')),
 title TEXT NOT NULL CHECK(char_length(title) BETWEEN 1 AND 100),
 url TEXT NOT NULL CHECK(char_length(url) BETWEEN 1 AND 2048 AND url ~ '^https?://'),
 summary TEXT NOT NULL DEFAULT '' CHECK(char_length(summary)<=2000),
 provider TEXT NOT NULL DEFAULT '' CHECK(char_length(provider)<=100), reference TEXT NOT NULL DEFAULT '' CHECK(char_length(reference)<=200),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id), UNIQUE(workspace_id,project_id,id)
);
CREATE INDEX idx_project_resources_page ON project_resources(workspace_id,project_id,archived,uuid);
CREATE TRIGGER trg_project_resources_updated_at BEFORE UPDATE ON project_resources FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE FUNCTION protect_project_contribution_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.workspace_id,NEW.project_id,NEW.team_id) IS DISTINCT FROM ROW(OLD.workspace_id,OLD.project_id,OLD.team_id) THEN
  RAISE EXCEPTION 'project contribution identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_project_contribution_identity BEFORE UPDATE ON project_teams FOR EACH ROW EXECUTE FUNCTION protect_project_contribution_identity();
CREATE FUNCTION protect_project_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.uuid,NEW.workspace_id) IS DISTINCT FROM ROW(OLD.uuid,OLD.workspace_id) THEN
  RAISE EXCEPTION 'project identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_project_identity BEFORE UPDATE ON projects FOR EACH ROW EXECUTE FUNCTION protect_project_identity();
CREATE FUNCTION protect_project_child_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.uuid,NEW.workspace_id,NEW.project_id) IS DISTINCT FROM ROW(OLD.uuid,OLD.workspace_id,OLD.project_id) THEN
  RAISE EXCEPTION 'project child identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_project_milestone_identity BEFORE UPDATE ON project_milestones FOR EACH ROW EXECUTE FUNCTION protect_project_child_identity();
CREATE TRIGGER trg_project_resource_identity BEFORE UPDATE ON project_resources FOR EACH ROW EXECUTE FUNCTION protect_project_child_identity();
ALTER TABLE workspace_history ADD COLUMN project_uuid UUID;
UPDATE event_consumers SET event_names=event_names || ARRAY['projects.created','projects.updated','projects.archived','projects.restored','project_teams.updated','project_milestones.created','project_milestones.updated','project_milestones.archived','project_milestones.restored','project_resources.created','project_resources.updated','project_resources.archived','project_resources.restored'] WHERE name='workspace.activity' AND version=1;
