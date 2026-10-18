-- Data-loss note: removes all issue relationship edges, revisions and relationship history.
UPDATE event_consumers SET event_names=array_remove(array_remove(array_remove(array_remove(array_remove(array_remove(event_names,'issue_relations.parent_set'),'issue_relations.duplicate_set'),'issue_relations.block_linked'),'issue_relations.block_unlinked'),'issue_relations.related_linked'),'issue_relations.related_unlinked') WHERE name='workspace.activity' AND version=1;
DROP FUNCTION relation_state(UUID,UUID);
DROP VIEW relation_public;
DROP VIEW relation_issue_reference;
DROP TABLE issue_relation_history;
DROP TABLE issue_relations;
DROP FUNCTION protect_issue_relation();
DROP TABLE issue_relation_revisions;
