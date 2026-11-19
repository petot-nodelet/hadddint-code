-- name: ListActivePaymentMethods :many
SELECT id, name, bank_name, account_number, account_holder, instructions
FROM payment_methods
WHERE is_active = 1
ORDER BY sort_order, id;

-- name: GetActivePaymentMethod :one
SELECT id, name, bank_name, account_number, account_holder, instructions
FROM payment_methods
WHERE id = ? AND is_active = 1;
