-- Data-loss note: removes durable replay receipts and workspace version history.
DROP TABLE operation_receipts;
ALTER TABLE workspaces DROP COLUMN version;
