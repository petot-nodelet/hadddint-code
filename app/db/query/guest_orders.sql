-- name: CreateGuestOrder :one
INSERT INTO orders (
    package_id, template_id, status, order_number, tracking_code, customer_name,
    customer_contact, public_name, price_cents, payment_status, work_status,
    draft_data, draft_access_hash
)
SELECT p.id, t.id, 'pending_payment', ?, ?, ?, ?, ?, p.price_cents,
       'pending_payment', 'waiting_payment', '{}', ?
FROM packages AS p
JOIN template_packages AS tp ON tp.package_id = p.id
JOIN templates AS t ON t.id = tp.template_id
WHERE p.id = @package_id AND t.id = @template_id
  AND p.available = 1 AND p.coming_soon = 0 AND t.is_active = 1
ON CONFLICT DO NOTHING
RETURNING id;

-- name: GetGuestOrderByNumber :one
SELECT o.id, o.order_number, o.tracking_code, o.customer_name, o.customer_contact,
       o.public_name, o.price_cents, o.payment_status, o.work_status, o.public_note,
       o.invitation_slug, o.draft_data, o.draft_access_hash, o.created_at, o.updated_at,
       o.package_id, p.name AS package_name, o.template_id, t.name AS template_name,
       COALESCE(pay.reject_reason, '') AS reject_reason,
       COALESCE(pay.proof_stored_name, '') AS proof_stored_name
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
LEFT JOIN payments AS pay ON pay.order_id = o.id
WHERE o.order_number = ?;

-- name: GetTrackedOrderByCode :one
SELECT o.order_number, o.public_name, o.price_cents, o.payment_status,
       o.work_status, o.public_note, o.invitation_slug, o.created_at,
       p.name AS package_name, t.name AS template_name
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
WHERE o.tracking_code = ?;

-- name: ListOrdersForAdmin :many
SELECT o.id, o.order_number, o.tracking_code, o.customer_name, o.customer_contact,
       o.public_name, o.price_cents, o.payment_status, o.work_status, o.public_note,
       o.internal_note,
       o.invitation_slug, o.created_at, o.updated_at, p.name AS package_name,
       t.name AS template_name, pay.id AS payment_id, pay.status AS payment_record_status,
       pay.proof_stored_name, pay.reject_reason, pm.name AS payment_method_name
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
LEFT JOIN payments AS pay ON pay.order_id = o.id
LEFT JOIN payment_methods AS pm ON pm.id = pay.payment_method_id
WHERE (CAST(@search AS TEXT) = ''
       OR COALESCE(o.order_number, '') LIKE '%' || CAST(@search AS TEXT) || '%'
       OR COALESCE(o.tracking_code, '') LIKE '%' || CAST(@search AS TEXT) || '%')
  AND (CAST(@payment_status AS TEXT) = '' OR o.payment_status = CAST(@payment_status AS TEXT))
  AND (CAST(@work_status AS TEXT) = '' OR o.work_status = CAST(@work_status AS TEXT))
ORDER BY o.id DESC
LIMIT @page_size OFFSET @page_offset;

-- name: GetAdminOrderByNumber :one
SELECT o.id, o.order_number, o.tracking_code, o.customer_name, o.customer_contact,
       o.public_name, o.price_cents, o.payment_status, o.work_status, o.public_note,
       o.internal_note,
       o.invitation_slug, o.created_at, o.updated_at, p.name AS package_name,
       t.name AS template_name, pay.id AS payment_id, pay.status AS payment_record_status,
       pay.proof_stored_name, pay.proof_mime_type, pay.proof_filename,
       pay.reject_reason, pm.name AS payment_method_name
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
LEFT JOIN payments AS pay ON pay.order_id = o.id
LEFT JOIN payment_methods AS pm ON pm.id = pay.payment_method_id
WHERE o.order_number = ?;

-- name: SetOrderPaymentSubmitted :execrows
UPDATE orders
SET payment_status = 'payment_submitted',
    status = 'payment_submitted',
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND payment_status IN ('pending_payment', 'payment_rejected')
  AND work_status = 'waiting_payment';

-- name: SetOrderPaymentPaid :execrows
UPDATE orders
SET payment_status = 'paid',
    status = 'paid',
    work_status = 'processing',
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND payment_status = 'payment_submitted'
  AND work_status = 'waiting_payment';

-- name: SetOrderPaymentRejected :execrows
UPDATE orders
SET payment_status = 'payment_rejected',
    status = 'pending_payment',
    work_status = 'waiting_payment',
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND payment_status = 'payment_submitted'
  AND work_status = 'waiting_payment';

-- name: SetOrderWorkStatus :execrows
UPDATE orders
SET work_status = @new_status,
    status = CASE WHEN @new_status = 'cancelled' THEN 'cancelled' ELSE status END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = @id AND work_status = @old_status
  AND @new_status IN ('cancelled', 'expired')
  AND work_status IN ('draft', 'waiting_payment')
  AND payment_status IN ('pending_payment', 'payment_rejected');

-- name: UpdateOrderPublicNote :execrows
UPDATE orders
SET public_note = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateOrderInternalNote :execrows
UPDATE orders
SET internal_note = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetOrderStatusesByID :one
SELECT payment_status, work_status
FROM orders
WHERE id = ?;

-- name: GetOrderNumberByPaymentID :one
SELECT o.order_number
FROM payments AS pay
JOIN orders AS o ON o.id = pay.order_id
WHERE pay.id = ?;

-- name: RecordPaymentReview :exec
INSERT INTO payment_reviews (payment_id, order_id, admin_id, decision, reason)
VALUES (?, ?, ?, ?, ?);

-- name: RecordOrderStatusHistory :exec
INSERT INTO order_status_history (
    order_id, actor_user_id, actor_type, old_payment_status, new_payment_status,
    old_work_status, new_work_status, note
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPaymentProofForAdmin :one
SELECT proof_stored_name, proof_mime_type, proof_filename
FROM payments
WHERE id = ? AND proof_stored_name != '';
