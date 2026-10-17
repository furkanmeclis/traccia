-- Data-loss note: removes durable consumer receipts, workspace history and activity projection.
DROP TABLE workspace_activity;
DROP TABLE workspace_history;
DROP FUNCTION protect_workspace_history();
DROP TABLE event_deliveries;
DROP TABLE event_consumers;
DROP TRIGGER trg_outbox_identity ON outbox_events;
DROP FUNCTION protect_outbox_identity();
ALTER TABLE outbox_events DROP CONSTRAINT uq_outbox_event_workspace,
 DROP COLUMN workspace_uuid, DROP COLUMN actor_uuid, DROP COLUMN event_version, DROP COLUMN occurred_at;
