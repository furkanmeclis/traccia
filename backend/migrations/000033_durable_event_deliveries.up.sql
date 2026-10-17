-- PostgreSQL owns event identity and each consumer's independent receipt.
ALTER TABLE outbox_events ADD COLUMN workspace_uuid UUID REFERENCES workspaces(uuid),
    ADD COLUMN actor_uuid UUID REFERENCES users(uuid),
    ADD COLUMN event_version INTEGER NOT NULL DEFAULT 1 CHECK (event_version > 0),
    ADD COLUMN occurred_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE outbox_events ADD CONSTRAINT uq_outbox_event_workspace UNIQUE (id, workspace_uuid);
CREATE FUNCTION protect_outbox_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.uuid,NEW.workspace_uuid,NEW.actor_uuid,NEW.event_name,NEW.event_version,NEW.occurred_at,NEW.payload)
 IS DISTINCT FROM ROW(OLD.uuid,OLD.workspace_uuid,OLD.actor_uuid,OLD.event_name,OLD.event_version,OLD.occurred_at,OLD.payload) THEN
  RAISE EXCEPTION 'event identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_outbox_identity BEFORE UPDATE ON outbox_events FOR EACH ROW EXECUTE FUNCTION protect_outbox_identity();
CREATE TABLE event_consumers (
    name TEXT NOT NULL CHECK (name <> ''), version INTEGER NOT NULL CHECK (version > 0),
    event_names TEXT[] NOT NULL CHECK (cardinality(event_names) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (name, version)
);
INSERT INTO event_consumers(name,version,event_names) VALUES ('workspace.activity',1,ARRAY['workspaces.created','workspaces.updated']);
CREATE TABLE event_deliveries (
    id BIGSERIAL PRIMARY KEY, uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    event_id BIGINT NOT NULL, workspace_uuid UUID NOT NULL REFERENCES workspaces(uuid),
    consumer_name TEXT NOT NULL, consumer_version INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','succeeded','quarantined')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(), lease_until TIMESTAMPTZ,
    lease_token UUID, error_code TEXT NOT NULL DEFAULT '', succeeded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (event_id,workspace_uuid) REFERENCES outbox_events(id,workspace_uuid),
    FOREIGN KEY (consumer_name,consumer_version) REFERENCES event_consumers(name,version),
    UNIQUE(event_id,consumer_name,consumer_version),
    CHECK ((status='processing') = (lease_token IS NOT NULL AND lease_until IS NOT NULL)),
    CHECK ((status='succeeded') = (succeeded_at IS NOT NULL))
);
CREATE INDEX idx_event_deliveries_claim ON event_deliveries(available_at,id) WHERE status IN ('pending','processing');
CREATE INDEX idx_event_deliveries_workspace ON event_deliveries(workspace_uuid,id DESC);
CREATE TRIGGER trg_event_deliveries_updated_at BEFORE UPDATE ON event_deliveries FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE workspace_history (
    id BIGSERIAL PRIMARY KEY, event_id BIGINT NOT NULL UNIQUE,
    uuid UUID NOT NULL UNIQUE, workspace_uuid UUID NOT NULL REFERENCES workspaces(uuid),
    actor_uuid UUID NOT NULL REFERENCES users(uuid), event_name TEXT NOT NULL,
    name TEXT NOT NULL, version BIGINT NOT NULL CHECK(version > 0),
    occurred_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (event_id,workspace_uuid) REFERENCES outbox_events(id,workspace_uuid),
    UNIQUE(id,event_id,workspace_uuid)
);
CREATE INDEX idx_workspace_history_workspace ON workspace_history(workspace_uuid,id DESC);
CREATE TABLE workspace_activity (
    id BIGINT PRIMARY KEY REFERENCES workspace_history(id),
    event_id BIGINT NOT NULL UNIQUE REFERENCES workspace_history(event_id),
    workspace_uuid UUID NOT NULL REFERENCES workspaces(uuid), created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY(id,event_id,workspace_uuid) REFERENCES workspace_history(id,event_id,workspace_uuid)
);
CREATE INDEX idx_workspace_activity_workspace ON workspace_activity(workspace_uuid,id DESC);

CREATE FUNCTION protect_workspace_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'workspace history is immutable'; END $$;
CREATE TRIGGER trg_workspace_history_identity BEFORE UPDATE OR DELETE ON workspace_history
 FOR EACH ROW EXECUTE FUNCTION protect_workspace_history();
