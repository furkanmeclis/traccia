-- Data-loss note: dropping project tables removes project settings, contributors, milestones and resource metadata; project context is also removed from historical rows.
UPDATE event_consumers SET event_names=ARRAY(SELECT e FROM unnest(event_names) e WHERE e NOT LIKE 'projects.%' AND e NOT LIKE 'project_teams.%' AND e NOT LIKE 'project_milestones.%' AND e NOT LIKE 'project_resources.%') WHERE name='workspace.activity' AND version=1;
ALTER TABLE workspace_history DROP COLUMN project_uuid;
DROP TABLE project_resources,project_milestones,project_teams,projects;
DROP FUNCTION protect_project_child_identity();
DROP FUNCTION protect_project_contribution_identity();
DROP FUNCTION protect_project_identity();
