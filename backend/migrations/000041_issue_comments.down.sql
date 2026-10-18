-- Data-loss note: rollback removes comment content, revisions and references.
UPDATE event_consumers SET event_names=array_remove(array_remove(array_remove(array_remove(array_remove(event_names,'comments.created'),'comments.replaced'),'comments.patched'),'comments.deleted'),'comments.restored') WHERE name='workspace.activity' AND version=1;
DROP VIEW issue_timeline_events;
DROP INDEX idx_workspace_history_issue_cursor;
ALTER TABLE workspace_history DROP COLUMN source_channel;
DROP VIEW comment_reference_public;
DROP TABLE comment_references;
ALTER TABLE issue_comments DROP CONSTRAINT fk_comment_head;
DROP TABLE comment_revisions;
DROP TABLE issue_comments;
DROP FUNCTION protect_comment_reference_identity();
DROP FUNCTION protect_comment_identity();
