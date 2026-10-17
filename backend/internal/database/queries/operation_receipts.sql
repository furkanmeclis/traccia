-- name: ClaimOperationReceipt :execrows
INSERT INTO operation_receipts(actor_id,workspace_id,operation_id,operation_version,idempotency_key,fingerprint,encrypted)
VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING;

-- name: GetOperationReceipt :one
SELECT * FROM operation_receipts WHERE actor_id=$1 AND workspace_id IS NOT DISTINCT FROM sqlc.narg(workspace_id)::uuid
AND operation_id=$2 AND operation_version=$3 AND idempotency_key=$4;

-- name: CompleteOperationReceipt :exec
UPDATE operation_receipts SET result=$2 WHERE id=$1;
