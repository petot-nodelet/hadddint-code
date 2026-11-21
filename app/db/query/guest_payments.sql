-- name: SaveGuestPaymentProof :execrows
INSERT INTO payments (
    order_id, user_id, payment_method_id, amount_cents, currency, status,
    proof_filename, proof_stored_name, proof_mime_type, proof_size_bytes,
    proof_uploaded_at, reject_reason, reviewed_by, reviewed_at
)
SELECT o.id, o.user_id, pm.id, o.price_cents, 'IDR', 'payment_submitted',
       ?, ?, ?, ?, CURRENT_TIMESTAMP, '', NULL, NULL
FROM orders AS o
JOIN payment_methods AS pm ON pm.id = @payment_method_id
WHERE o.id = @order_id AND pm.is_active = 1 AND o.payment_status = 'payment_submitted'
ON CONFLICT (order_id) DO UPDATE SET
    payment_method_id = excluded.payment_method_id,
    amount_cents = excluded.amount_cents,
    status = 'payment_submitted',
    proof_filename = excluded.proof_filename,
    proof_stored_name = excluded.proof_stored_name,
    proof_mime_type = excluded.proof_mime_type,
    proof_size_bytes = excluded.proof_size_bytes,
    proof_uploaded_at = CURRENT_TIMESTAMP,
    reject_reason = '',
    reviewed_by = NULL,
    reviewed_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE payments.status IN ('pending_payment', 'rejected');
