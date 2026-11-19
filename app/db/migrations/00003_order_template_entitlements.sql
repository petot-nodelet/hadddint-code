-- +goose Up
CREATE TABLE orders_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    package_id  TEXT NOT NULL REFERENCES packages(id),
    template_id TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending', 'paid', 'cancelled')),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (template_id, package_id) REFERENCES template_packages(template_id, package_id)
);

INSERT INTO orders_new (id, user_id, package_id, template_id, status, created_at, updated_at)
SELECT id, user_id, package_id, template_id, status, created_at, updated_at
FROM orders;

DROP TABLE orders;
ALTER TABLE orders_new RENAME TO orders;
CREATE INDEX idx_orders_user_status ON orders (user_id, status);

-- +goose Down
CREATE TABLE orders_old (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    package_id  TEXT NOT NULL REFERENCES packages(id),
    template_id TEXT NOT NULL REFERENCES templates(id),
    status      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending', 'paid', 'cancelled')),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO orders_old (id, user_id, package_id, template_id, status, created_at, updated_at)
SELECT id, user_id, package_id, template_id, status, created_at, updated_at
FROM orders;

DROP TABLE orders;
ALTER TABLE orders_old RENAME TO orders;
CREATE INDEX idx_orders_user_status ON orders (user_id, status);
