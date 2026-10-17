-- name: InsertOutboxEvent :execrows
INSERT INTO outbox_events(uuid,event_name,payload,workspace_uuid,actor_uuid,event_version,occurred_at)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(uuid) DO NOTHING;

-- name: DatabaseTime :one
SELECT clock_timestamp()::timestamptz;

-- name: OutboxPayloadMatches :one
SELECT payload=sqlc.arg(payload)::jsonb FROM outbox_events WHERE uuid=sqlc.arg(uuid);
