-- name: GetOrderForUpdateTx :one
SELECT o.id, o.user_id, o.package_id, p.name AS package_name, p.price_cents,
       p.duration_days, o.template_id, t.name AS template_name, o.status
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
WHERE o.id = ? AND o.user_id = ?;

-- name: SetOrderStatusIfTx :execrows
UPDATE orders
SET status = @new_status, updated_at = CURRENT_TIMESTAMP
WHERE id = @id AND status IN (@allowed_a, @allowed_b);

-- name: ListOrdersForUserWithPayments :many
SELECT o.id, o.package_id, p.name AS package_name, p.price_cents,
       p.duration_days, o.template_id, t.name AS template_name, o.status,
       pay.id AS payment_id, pay.status AS payment_status,
       COALESCE(pm.name, '') AS method_name
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
LEFT JOIN payments AS pay ON pay.order_id = o.id
LEFT JOIN payment_methods AS pm ON pm.id = pay.payment_method_id
WHERE o.user_id = ?
ORDER BY o.id DESC;
