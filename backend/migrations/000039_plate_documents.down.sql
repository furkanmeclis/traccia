-- Data-loss note: removes canonical issue/project document bodies, revision journals and reference anchors.
UPDATE event_consumers SET event_names=array_remove(array_remove(array_remove(array_remove(event_names,'documents.created'),'documents.replaced'),'documents.patched'),'documents.restored') WHERE name='workspace.activity' AND version=1;
DROP VIEW document_reference_public;
DROP VIEW document_owners;
DROP TABLE document_references;
DROP FUNCTION protect_document_reference_identity();
ALTER TABLE documents DROP CONSTRAINT fk_document_head;
DROP TABLE document_revisions;
DROP TABLE documents;
DROP FUNCTION protect_document_identity();
