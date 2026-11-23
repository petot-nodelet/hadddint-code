-- +goose Up
ALTER TABLE orders
    ADD COLUMN draft_version INTEGER NOT NULL DEFAULT 1 CHECK (draft_version > 0);

ALTER TABLE orders
    ADD COLUMN invitation_review_status TEXT NOT NULL DEFAULT 'in_progress'
    CHECK (invitation_review_status IN ('in_progress', 'revision_requested', 'ready'));

ALTER TABLE orders
    ADD COLUMN invitation_review_note TEXT NOT NULL DEFAULT '';

CREATE TABLE invitation_media (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    media_type  TEXT NOT NULL CHECK (media_type IN ('portrait', 'cover', 'story', 'gallery', 'audio')),
    stored_name TEXT NOT NULL UNIQUE,
    mime_type   TEXT NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp', 'audio/mpeg', 'audio/ogg')),
    size_bytes  INTEGER NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 10485760),
    caption     TEXT NOT NULL DEFAULT '',
    sort_order  INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_invitation_media_order_type ON invitation_media (order_id, media_type, sort_order, id);

CREATE INDEX idx_orders_invitation_review ON orders (invitation_review_status, updated_at);

CREATE TABLE music_tracks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    title       TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    stored_name TEXT NOT NULL UNIQUE,
    mime_type   TEXT NOT NULL CHECK (mime_type IN ('audio/mpeg', 'audio/ogg')),
    size_bytes  INTEGER NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 5242880),
    is_active   INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_music_tracks_active ON music_tracks (is_active, title, id);

-- +goose Down
DROP INDEX IF EXISTS idx_music_tracks_active;
DROP TABLE IF EXISTS music_tracks;
DROP INDEX IF EXISTS idx_orders_invitation_review;
DROP TABLE IF EXISTS invitation_media;
ALTER TABLE orders DROP COLUMN invitation_review_note;
ALTER TABLE orders DROP COLUMN invitation_review_status;
ALTER TABLE orders DROP COLUMN draft_version;
