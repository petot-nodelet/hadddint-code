-- +goose Up
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    full_name     TEXT NOT NULL,
    email         TEXT NOT NULL COLLATE NOCASE UNIQUE,
    phone         TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('customer', 'admin')),
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_role ON users (role);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    data       BLOB NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE INDEX idx_sessions_expires ON sessions (expires_at);

CREATE TABLE packages (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    tagline       TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    price_cents   INTEGER NOT NULL CHECK (price_cents >= 0),
    currency      TEXT NOT NULL DEFAULT 'IDR',
    duration_days INTEGER NOT NULL DEFAULT 90 CHECK (duration_days > 0),
    max_photos    INTEGER NOT NULL DEFAULT 0,
    max_videos    INTEGER NOT NULL DEFAULT 0,
    max_events    INTEGER NOT NULL DEFAULT 1,
    coming_soon   INTEGER NOT NULL DEFAULT 0 CHECK (coming_soon IN (0, 1)),
    available     INTEGER NOT NULL DEFAULT 1 CHECK (available IN (0, 1)),
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE package_features (
    package_id             TEXT PRIMARY KEY REFERENCES packages(id) ON DELETE CASCADE,
    rsvp_enabled           INTEGER NOT NULL DEFAULT 0,
    guestbook_enabled      INTEGER NOT NULL DEFAULT 0,
    digital_envelope       INTEGER NOT NULL DEFAULT 0,
    whatsapp_button        INTEGER NOT NULL DEFAULT 0,
    background_music       INTEGER NOT NULL DEFAULT 0,
    countdown              INTEGER NOT NULL DEFAULT 0,
    guest_greeting         INTEGER NOT NULL DEFAULT 0,
    couple_story           INTEGER NOT NULL DEFAULT 0,
    custom_colors          INTEGER NOT NULL DEFAULT 0,
    custom_fonts           INTEGER NOT NULL DEFAULT 0,
    custom_layout          INTEGER NOT NULL DEFAULT 0,
    custom_cover           INTEGER NOT NULL DEFAULT 0,
    custom_background      INTEGER NOT NULL DEFAULT 0,
    custom_sections        INTEGER NOT NULL DEFAULT 0,
    custom_wording         INTEGER NOT NULL DEFAULT 0,
    custom_music           INTEGER NOT NULL DEFAULT 0,
    custom_url             INTEGER NOT NULL DEFAULT 0,
    multiple_events        INTEGER NOT NULL DEFAULT 0,
    map_preview            INTEGER NOT NULL DEFAULT 0,
    event_customization    INTEGER NOT NULL DEFAULT 0,
    live_rsvp              INTEGER NOT NULL DEFAULT 0,
    guest_list             INTEGER NOT NULL DEFAULT 0,
    qr_code                INTEGER NOT NULL DEFAULT 0,
    social_sharing_preview INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE templates (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT 'general',
    description TEXT NOT NULL DEFAULT '',
    thumbnail   TEXT NOT NULL DEFAULT '',
    is_active   INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    config      TEXT NOT NULL DEFAULT '{}',
    features    TEXT NOT NULL DEFAULT '[]',
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE template_packages (
    template_id TEXT NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    package_id  TEXT NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    PRIMARY KEY (template_id, package_id)
);

CREATE INDEX idx_template_packages_package ON template_packages (package_id, template_id);
CREATE INDEX idx_templates_active_sort ON templates (is_active, sort_order, id);
CREATE INDEX idx_templates_category ON templates (category);

CREATE TABLE orders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    package_id TEXT NOT NULL REFERENCES packages(id),
    template_id TEXT NOT NULL REFERENCES templates(id),
    status     TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending', 'paid', 'cancelled')),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_orders_user_status ON orders (user_id, status);

-- +goose Down
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS template_packages;
DROP TABLE IF EXISTS templates;
DROP TABLE IF EXISTS package_features;
DROP TABLE IF EXISTS packages;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
