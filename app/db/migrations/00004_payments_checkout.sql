-- +goose Up
-- Phase 3: order status flow, payment methods, payments, and notifications.

-- Expand the order status vocabulary to the Phase 3 flow. Legacy values are
-- mapped explicitly ('pending' -> 'pending_payment') so existing rows survive.
CREATE TABLE orders_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    package_id  TEXT NOT NULL REFERENCES packages(id),
    template_id TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending_payment', 'payment_submitted', 'paid', 'cancelled')),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (template_id, package_id) REFERENCES template_packages(template_id, package_id)
);

INSERT INTO orders_new (id, user_id, package_id, template_id, status, created_at, updated_at)
SELECT id, user_id, package_id, template_id,
       CASE status WHEN 'pending' THEN 'pending_payment' ELSE status END,
       created_at, updated_at
FROM orders;

DROP TABLE orders;
ALTER TABLE orders_new RENAME TO orders;
CREATE INDEX idx_orders_user_status ON orders (user_id, status);
CREATE INDEX idx_orders_status ON orders (status);

CREATE TABLE payment_methods (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    bank_name      TEXT NOT NULL DEFAULT '',
    account_number TEXT NOT NULL DEFAULT '',
    account_holder TEXT NOT NULL DEFAULT '',
    instructions   TEXT NOT NULL DEFAULT '',
    is_active      INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    sort_order     INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO payment_methods (id, name, bank_name, account_number, account_holder, instructions, is_active, sort_order) VALUES
('bank_transfer', 'Transfer Bank', 'BCA', '8290-3341-556', 'PT SukaMoto Kreasi Digital',
 '1. Transfer total pembayaran ke rekening di atas. 2. Gunakan nominal persis sampai tiga digit terakhir sebagai kode unik. 3. Simpan tangkapan layar bukti transfer. 4. Unggah bukti pada halaman ini.',
 1, 1),
('ewallet', 'E-Wallet (GoPay / OVO / Dana)', 'GoPay', '0812-3456-7890', 'PT SukaMoto Kreasi Digital',
 '1. Kirim pembayaran ke nomor e-wallet di atas. 2. Pastikan nominal sesuai total. 3. Simpan tangkapan layar bukti transfer. 4. Unggah bukti pada halaman ini.',
 1, 2),
('qris', 'QRIS', '', 'Statik SUKAMOTO', 'PT SukaMoto Kreasi Digital',
 '1. Minta QR statis melalui WhatsApp resmi SUKAMOTO. 2. Bayar sesuai total menggunakan aplikasi apa pun. 3. Simpan tangkapan layar bukti bayar. 4. Unggah bukti pada halaman ini.',
 1, 3);

CREATE TABLE payments (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id          INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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

CREATE INDEX idx_payments_status ON payments (status, id);
CREATE INDEX idx_payments_user ON payments (user_id, id);

CREATE TABLE notifications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id   INTEGER REFERENCES orders(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    message    TEXT NOT NULL,
    is_read    INTEGER NOT NULL DEFAULT 0 CHECK (is_read IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_notifications_user ON notifications (user_id, is_read, id);

-- +goose Down
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS payment_methods;

CREATE TABLE orders_old (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    package_id  TEXT NOT NULL REFERENCES packages(id),
    template_id TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending', 'paid', 'cancelled')),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (template_id, package_id) REFERENCES template_packages(template_id, package_id)
);

INSERT INTO orders_old (id, user_id, package_id, template_id, status, created_at, updated_at)
SELECT id, user_id, package_id, template_id,
       CASE status
           WHEN 'pending_payment' THEN 'pending'
           WHEN 'payment_submitted' THEN 'pending'
           ELSE status
       END,
       created_at, updated_at
FROM orders
WHERE status NOT IN ('cancelled');

DROP TABLE orders;
ALTER TABLE orders_old RENAME TO orders;
CREATE INDEX idx_orders_user_status ON orders (user_id, status);
