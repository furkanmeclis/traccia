-- name: InsertEventDeliveries :exec
INSERT INTO event_deliveries(event_id,workspace_uuid,consumer_name,consumer_version)
SELECT sqlc.arg(event_id),sqlc.arg(workspace_uuid),name,version FROM event_consumers
WHERE sqlc.arg(event_name)::text = ANY(event_names)
ON CONFLICT(event_id,consumer_name,consumer_version) DO NOTHING;

-- name: ClaimEventDeliveries :many
WITH picked AS (
 SELECT id FROM event_deliveries
 WHERE status='pending' AND available_at <= clock_timestamp()
 ORDER BY available_at,id LIMIT sqlc.arg(batch_limit) FOR UPDATE SKIP LOCKED
)
UPDATE event_deliveries d SET status='processing', attempts=attempts+1,
 lease_token=gen_random_uuid(),lease_until=clock_timestamp()+(sqlc.arg(lease_ms)::bigint*interval '1 millisecond'),error_code=''
FROM picked WHERE d.id=picked.id RETURNING d.*;

-- name: LockEventDelivery :one
SELECT * FROM event_deliveries WHERE id=$1 AND status='processing' AND lease_token=$2
 AND lease_until > clock_timestamp() FOR UPDATE;

-- name: CompleteEventDelivery :execrows
UPDATE event_deliveries SET status='succeeded',succeeded_at=clock_timestamp(),lease_token=NULL,lease_until=NULL,error_code=''
WHERE id=$1 AND status='processing' AND lease_token=$2 AND lease_until > clock_timestamp();

-- name: RetryEventDelivery :execrows
UPDATE event_deliveries SET status=CASE WHEN attempts >= sqlc.arg(max_attempts)::integer THEN 'quarantined' ELSE 'pending' END,
 available_at=clock_timestamp()+(LEAST(300,power(2,LEAST(attempts-1,9))) * interval '1 second'),
 error_code=sqlc.arg(error_code),lease_token=NULL,lease_until=NULL
WHERE id=sqlc.arg(id) AND status='processing' AND lease_token=sqlc.arg(lease_token) AND lease_until > clock_timestamp();

-- name: ReplayEventDelivery :one
UPDATE event_deliveries SET status='pending',attempts=0,available_at=clock_timestamp(),lease_token=NULL,lease_until=NULL,error_code=''
WHERE uuid=sqlc.arg(delivery_uuid) AND workspace_uuid=sqlc.arg(workspace_uuid)
 AND (status IN ('pending','quarantined') OR (status='processing' AND lease_until <= clock_timestamp()))
RETURNING *;

-- name: ListEventDeliveries :many
SELECT d.*,o.uuid AS event_uuid,o.event_name FROM event_deliveries d JOIN outbox_events o ON o.id=d.event_id
WHERE d.workspace_uuid=sqlc.arg(workspace_uuid)
 AND (sqlc.arg(after_uuid)::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR d.id < (SELECT c.id FROM event_deliveries c WHERE c.uuid=sqlc.arg(after_uuid) AND c.workspace_uuid=sqlc.arg(workspace_uuid)))
ORDER BY d.id DESC LIMIT sqlc.arg(row_limit);

-- name: GetOutboxEvent :one
SELECT * FROM outbox_events WHERE id=$1;

-- name: GetOutboxEventByUUID :one
SELECT * FROM outbox_events WHERE uuid=$1;


-- name: ReclaimExpiredEventDeliveries :execrows
WITH picked AS (
 SELECT id FROM event_deliveries WHERE status='processing' AND lease_until <= clock_timestamp()
 ORDER BY lease_until,id LIMIT 100 FOR UPDATE SKIP LOCKED
)
UPDATE event_deliveries d SET status=CASE WHEN attempts>=sqlc.arg(max_attempts)::integer THEN 'quarantined' ELSE 'pending' END,
 available_at=clock_timestamp()+(LEAST(300,power(2,LEAST(attempts-1,9)))*interval '1 second'),
 lease_token=NULL,lease_until=NULL,error_code='lease_expired'
FROM picked WHERE d.id=picked.id;
