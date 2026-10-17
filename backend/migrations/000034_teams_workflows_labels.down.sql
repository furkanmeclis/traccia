-- Data-loss note: removes team configuration, labels, counters and history resource metadata.
UPDATE event_consumers SET event_names=ARRAY['workspaces.created','workspaces.updated'] WHERE name='workspace.activity' AND version=1;
ALTER TABLE workspace_history DROP COLUMN team_uuid, DROP COLUMN resource_uuid, DROP COLUMN resource_type;
DROP TABLE labels;
DROP TABLE workflow_statuses;
DROP TABLE teams;
DROP FUNCTION protect_team_identity();
