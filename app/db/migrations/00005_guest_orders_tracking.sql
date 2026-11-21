-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

CREATE TABLE orders_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id             INTEGER REFERENCES users(id) ON DELETE CASCADE,
    package_id          TEXT NOT NULL REFERENCES packages(id),
    template_id         TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending_payment', 'payment_submitted', 'paid', 'cancelled')),
    order_number        TEXT UNIQUE,
    tracking_code       TEXT UNIQUE,
    customer_name       TEXT NOT NULL DEFAULT '',
    customer_contact    TEXT NOT NULL DEFAULT '',
    public_name         TEXT NOT NULL DEFAULT '',
    price_cents         INTEGER NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    payment_status      TEXT NOT NULL DEFAULT 'pending_payment' CHECK (payment_status IN ('pending_payment', 'payment_submitted', 'payment_rejected', 'paid', 'refunded')),
    work_status         TEXT NOT NULL DEFAULT 'draft' CHECK (work_status IN ('draft', 'waiting_payment', 'processing', 'published', 'expired', 'cancelled')),
    public_note         TEXT NOT NULL DEFAULT '',
    internal_note       TEXT NOT NULL DEFAULT '',
    invitation_slug     TEXT UNIQUE,
    draft_data          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(draft_data)),
    draft_access_hash   BLOB,
    created_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (template_id, package_id) REFERENCES template_packages(template_id, package_id)
);

INSERT INTO orders_new (
    id, user_id, package_id, template_id, status, customer_name,
    customer_contact, public_name, price_cents, payment_status, work_status,
    created_at, updated_at
)
SELECT o.id, o.user_id, o.package_id, o.template_id, o.status,
       COALESCE(u.full_name, ''),
       COALESCE(NULLIF(u.phone, ''), u.email, ''),
       '',
       p.price_cents,
       CASE o.status
           WHEN 'payment_submitted' THEN 'payment_submitted'
           WHEN 'paid' THEN 'paid'
           ELSE 'pending_payment'
       END,
       CASE o.status
           WHEN 'draft' THEN 'draft'
           WHEN 'paid' THEN 'processing'
           WHEN 'cancelled' THEN 'cancelled'
           ELSE 'waiting_payment'
       END,
       o.created_at, o.updated_at
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
LEFT JOIN users AS u ON u.id = o.user_id;

DROP TABLE orders;
ALTER TABLE orders_new RENAME TO orders;
CREATE INDEX idx_orders_user_status ON orders (user_id, status);
CREATE INDEX idx_orders_status ON orders (status);
CREATE INDEX idx_orders_payment_work_status ON orders (payment_status, work_status, updated_at);
CREATE INDEX idx_orders_search ON orders (order_number, tracking_code);

CREATE TABLE payments_new (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id          INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    user_id           INTEGER REFERENCES users(id) ON DELETE CASCADE,
    payment_method_id TEXT NOT NULL REFERENCES payment_methods(id),
    amount_cents      INTEGER NOT NULL CHECK (amount_cents > 0),
    currency          TEXT NOT NULL DEFAULT 'IDR',
    status            TEXT NOT NULL DEFAULT 'pending_payment' CHECK (status IN ('pending_payment', 'payment_submitted', 'verified', 'rejected')),
    proof_filename    TEXT NOT NULL DEFAULT '',
    proof_stored_name TEXT NOT NULL DEFAULT '',
    proof_mime_type   TEXT NOT NULL DEFAULT '',
    proof_size_bytes  INTEGER NOT NULL DEFAULT 0,
    proof_uploaded_at TEXT,
    reject_reason     TEXT NOT NULL DEFAULT '',
    reviewed_by       INTEGER REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at       TEXT,
    created_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (order_id)
);

INSERT INTO payments_new (
    id, order_id, user_id, payment_method_id, amount_cents, currency, status,
    proof_filename, proof_stored_name, proof_mime_type, proof_size_bytes,
    proof_uploaded_at, reject_reason, reviewed_by, reviewed_at, created_at, updated_at
)
SELECT id, order_id, user_id, payment_method_id, amount_cents, currency, status,
       proof_filename, proof_stored_name, proof_mime_type, proof_size_bytes,
       proof_uploaded_at, reject_reason, reviewed_by, reviewed_at, created_at, updated_at
FROM payments;

DROP TABLE payments;
ALTER TABLE payments_new RENAME TO payments;
CREATE INDEX idx_payments_status ON payments (status, id);
CREATE INDEX idx_payments_user ON payments (user_id, id);

CREATE TABLE payment_reviews (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    payment_id  INTEGER NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
    order_id    INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    admin_id    INTEGER NOT NULL REFERENCES users(id),
    decision    TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_payment_reviews_payment ON payment_reviews (payment_id, id);

CREATE TABLE order_status_history (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id             INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    actor_user_id        INTEGER REFERENCES users(id) ON DELETE SET NULL,
    actor_type           TEXT NOT NULL CHECK (actor_type IN ('system', 'admin')),
    old_payment_status   TEXT NOT NULL,
    new_payment_status   TEXT NOT NULL,
    old_work_status      TEXT NOT NULL,
    new_work_status      TEXT NOT NULL,
    note                 TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_order_status_history_order ON order_status_history (order_id, id);

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
-- Guest orders cannot be represented by the previous NOT NULL user_id schema.
-- Keep the expanded schema to avoid deleting or reassigning customer records.
