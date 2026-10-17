ALTER TABLE workspaces ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0);
CREATE TABLE operation_receipts (
 id BIGSERIAL PRIMARY KEY,
 actor_id UUID NOT NULL REFERENCES users(uuid),
 workspace_id UUID REFERENCES workspaces(uuid),
 operation_id TEXT NOT NULL,
 operation_version BIGINT NOT NULL CHECK (operation_version > 0),
 idempotency_key TEXT NOT NULL CHECK (octet_length(idempotency_key) BETWEEN 1 AND 200),
 fingerprint BYTEA NOT NULL CHECK (octet_length(fingerprint) = 32),
 result BYTEA,
 encrypted BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX uq_operation_receipts_account ON operation_receipts(actor_id,operation_id,operation_version,idempotency_key) WHERE workspace_id IS NULL;
CREATE UNIQUE INDEX uq_operation_receipts_workspace ON operation_receipts(actor_id,workspace_id,operation_id,operation_version,idempotency_key) WHERE workspace_id IS NOT NULL;
CREATE INDEX idx_operation_receipts_workspace ON operation_receipts(workspace_id);
CREATE TRIGGER trg_operation_receipts_updated_at BEFORE UPDATE ON operation_receipts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
