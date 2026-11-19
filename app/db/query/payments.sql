-- name: CreatePayment :one
INSERT INTO payments (order_id, user_id, payment_method_id, amount_cents, currency)
VALUES (?, ?, ?, ?, 'IDR')
RETURNING id;

-- name: GetPaymentByOrderID :one
SELECT id, order_id, user_id, payment_method_id, amount_cents, currency, status,
       proof_filename, proof_stored_name, proof_mime_type, proof_size_bytes,
       proof_uploaded_at, reject_reason, reviewed_by, reviewed_at, created_at, updated_at
FROM payments
WHERE order_id = ?;

-- name: GetPaymentByID :one
SELECT id, order_id, user_id, payment_method_id, amount_cents, currency, status,
       proof_filename, proof_stored_name, proof_mime_type, proof_size_bytes,
       proof_uploaded_at, reject_reason, reviewed_by, reviewed_at, created_at, updated_at
FROM payments
WHERE id = ?;

-- name: MarkPaymentSubmitted :execrows
UPDATE payments
SET status = 'payment_submitted',
    proof_filename = ?,
    proof_stored_name = ?,
    proof_mime_type = ?,
    proof_size_bytes = ?,
    proof_uploaded_at = CURRENT_TIMESTAMP,
    reject_reason = '',
    reviewed_by = NULL,
    reviewed_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status IN ('pending_payment', 'rejected');

-- name: VerifyPayment :execrows
UPDATE payments
SET status = 'verified',
    reject_reason = '',
    reviewed_by = ?,
    reviewed_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = 'payment_submitted';

-- name: RejectPayment :execrows
UPDATE payments
SET status = 'rejected',
    reject_reason = ?,
    reviewed_by = ?,
    reviewed_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = 'payment_submitted';

-- name: ListPaymentsForAdmin :many
SELECT pay.id AS payment_id, pay.order_id, pay.status AS payment_status,
       pay.amount_cents, pay.currency, pay.proof_stored_name, pay.reject_reason,
       pay.created_at AS payment_created_at, pay.updated_at AS payment_updated_at,
       o.user_id, u.full_name AS customer_name, u.email AS customer_email,
       u.phone AS customer_phone,
       p.name AS package_name, t.name AS template_name,
       pm.name AS method_name, o.status AS order_status
FROM payments AS pay
JOIN orders AS o ON o.id = pay.order_id
JOIN users AS u ON u.id = o.user_id
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
JOIN payment_methods AS pm ON pm.id = pay.payment_method_id
WHERE CAST(@status AS TEXT) = '' OR pay.status = CAST(@status AS TEXT)
ORDER BY pay.id DESC;

-- name: ListPaymentsForUser :many
SELECT pay.id AS payment_id, pay.order_id, pay.status AS payment_status,
       pay.amount_cents, pay.currency, pay.proof_stored_name, pay.reject_reason,
       pay.created_at AS payment_created_at, pay.updated_at AS payment_updated_at,
       o.user_id, u.full_name AS customer_name, u.email AS customer_email,
       u.phone AS customer_phone,
       p.name AS package_name, t.name AS template_name,
       pm.name AS method_name, o.status AS order_status
FROM payments AS pay
JOIN orders AS o ON o.id = pay.order_id
JOIN users AS u ON u.id = o.user_id
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
JOIN payment_methods AS pm ON pm.id = pay.payment_method_id
WHERE pay.user_id = ?
ORDER BY pay.id DESC;
