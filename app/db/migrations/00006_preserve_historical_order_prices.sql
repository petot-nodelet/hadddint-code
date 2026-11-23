-- +goose Up
-- Keep the amount originally recorded for legacy payments when the order
-- price was backfilled from a package whose price may have changed.
UPDATE orders
SET price_cents = (
    SELECT amount_cents
    FROM payments
    WHERE payments.order_id = orders.id
)
WHERE EXISTS (
    SELECT 1
    FROM payments
    WHERE payments.order_id = orders.id
);

-- +goose Down
-- Historical order prices cannot be safely reconstructed from mutable packages.
