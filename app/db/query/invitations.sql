-- name: GetInvitationDraftByOrderNumber :one
SELECT o.id, o.order_number, o.package_id, p.name AS package_name,
       p.max_photos, p.max_videos, p.max_events, o.template_id,
       t.name AS template_name, t.config AS template_config,
       o.draft_data, o.draft_version, o.invitation_slug,
       o.invitation_review_status, o.invitation_review_note,
       o.payment_status, o.work_status
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
WHERE o.order_number = ?;

-- name: GetInvitationPackageFeatures :one
SELECT rsvp_enabled, guestbook_enabled, digital_envelope, background_music,
       guest_greeting, couple_story, custom_colors, custom_fonts, custom_layout,
       custom_cover, custom_background, custom_sections, custom_wording,
       custom_music, custom_url, multiple_events, event_customization
FROM package_features
WHERE package_id = ?;

-- name: SaveInvitationDraft :execrows
UPDATE orders
SET draft_data = ?, draft_version = draft_version + 1,
    invitation_slug = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND draft_version = ?;

-- name: ListInvitationMedia :many
SELECT id, order_id, media_type, stored_name, mime_type, size_bytes,
       caption, sort_order, created_at
FROM invitation_media
WHERE order_id = ?
ORDER BY media_type, sort_order, id;

-- name: GetInvitationMedia :one
SELECT id, order_id, media_type, stored_name, mime_type, size_bytes,
       caption, sort_order, created_at
FROM invitation_media
WHERE id = ? AND order_id = ?;

-- name: InsertInvitationMedia :one
INSERT INTO invitation_media (order_id, media_type, stored_name, mime_type, size_bytes, caption, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: CountInvitationMediaByType :one
SELECT count(*)
FROM invitation_media
WHERE order_id = ? AND media_type IN ('portrait', 'cover', 'story', 'gallery');

-- name: CountInvitationAudio :one
SELECT count(*)
FROM invitation_media
WHERE order_id = ? AND media_type = 'audio';

-- name: DeleteInvitationMedia :execrows
DELETE FROM invitation_media
WHERE id = ? AND order_id = ?;

-- name: UpdateInvitationMediaCaption :execrows
UPDATE invitation_media
SET caption = ?, sort_order = ?
WHERE id = ? AND order_id = ? AND media_type IN ('story', 'gallery');

-- name: GetAdminInvitationDraft :one
SELECT o.order_number, o.package_id, p.name AS package_name, p.max_photos,
       p.max_videos, p.max_events, o.template_id, t.name AS template_name, t.config AS template_config,
       o.draft_data, o.draft_version, o.invitation_slug,
       o.invitation_review_status, o.invitation_review_note,
       o.payment_status, o.work_status, o.id
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
WHERE o.order_number = ?;

-- name: UpdateInvitationReview :execrows
UPDATE orders
SET invitation_review_status = ?, invitation_review_note = ?, updated_at = CURRENT_TIMESTAMP
WHERE order_number = ?;

-- name: ListActiveMusicTracks :many
SELECT id, title, stored_name, mime_type, size_bytes, is_active, created_at
FROM music_tracks
WHERE is_active = 1
ORDER BY title, id;

-- name: ListMusicTracks :many
SELECT id, title, stored_name, mime_type, size_bytes, is_active, created_at
FROM music_tracks
ORDER BY is_active DESC, title, id;

-- name: GetMusicTrack :one
SELECT id, title, stored_name, mime_type, size_bytes, is_active, created_at
FROM music_tracks
WHERE id = ?;

-- name: InsertMusicTrack :one
INSERT INTO music_tracks (title, stored_name, mime_type, size_bytes)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: SetMusicTrackActive :execrows
UPDATE music_tracks
SET is_active = ?
WHERE id = ?;

-- name: IsInvitationSlugAvailable :one
SELECT count(*) = 0
FROM orders
WHERE invitation_slug = ? AND id != ?;
