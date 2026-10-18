-- Data-loss note: issue identities, aliases, metadata and immutable history are removed.
UPDATE event_consumers SET event_names=array_remove(array_remove(array_remove(array_remove(array_remove(event_names,'issues.created'),'issues.updated'),'issues.moved'),'issues.archived'),'issues.restored') WHERE name='workspace.activity' AND version=1;
DROP VIEW issue_public;
DROP TABLE issue_history,issue_labels,issue_aliases,issues;
DROP FUNCTION protect_issue_alias(),protect_issue_identity();

ALTER TABLE workspace_history DROP COLUMN issue_uuid, DROP COLUMN issue_identifier;
