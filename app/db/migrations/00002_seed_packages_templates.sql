-- +goose Up
INSERT INTO packages (id, name, tagline, description, price_cents, duration_days, max_photos, max_videos, max_events, coming_soon, available, sort_order) VALUES
('silver', 'Silver', 'Simple & Elegant', 'Undangan digital siap pakai, tinggal isi data.', 15000000, 60, 8, 1, 2, 0, 1, 1),
('gold', 'Gold', 'Personalized & Premium', 'Template premium yang dapat disesuaikan dengan karakter pasangan.', 35000000, 90, 24, 3, 5, 0, 1, 2),
('platinum', 'Platinum', 'Coming Soon', 'Paket eksklusif dengan fitur lengkap. Segera hadir.', 75000000, 180, 60, 10, 10, 1, 0, 3);

INSERT INTO package_features (package_id,
    rsvp_enabled, guestbook_enabled, digital_envelope, whatsapp_button, background_music,
    countdown, guest_greeting, couple_story,
    custom_colors, custom_fonts, custom_layout, custom_cover, custom_background,
    custom_sections, custom_wording, custom_music, custom_url,
    multiple_events, map_preview, event_customization,
    live_rsvp, guest_list, qr_code, social_sharing_preview) VALUES
('silver', 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0),
('gold', 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1),
('platinum', 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1);

INSERT INTO templates (id, name, category, description, thumbnail, is_active, config, features, sort_order) VALUES
('S01', 'Floral Elegant', 'floral', 'Nuansa bunga pastel yang lembut dan klasik.', '/index/photo-1511285560929-80b456fea0bc.jpg', 1,
 '{"palette":{"background":"#201a18","surface":"#efeae2","accent":"#c9a27c","text":"#f8f4ee"},"typography":{"heading":"Alex Brush","body":"Plus Jakarta Sans"},"layout":"classic","cover_style":"framed","sections":["cover","couple","story","events","gallery","rsvp"],"media":{"cover":"/index/photo-1511285560929-80b456fea0bc.jpg","gallery":["/index/photo-1519741497674-611481863552.jpg"]}}',
 '["RSVP","Cerita pasangan","Galeri foto"]', 1),
('S02', 'Japanese Garden', 'japanese', 'Minimalisme taman Jepang dengan sakura dan garis bersih.', '/index/photo-1519741497674-611481863552.jpg', 1,
 '{"palette":{"background":"#1d1b19","surface":"#f5f1e8","accent":"#b3574f","text":"#f8f5ef"},"typography":{"heading":"Cormorant Garamond","body":"Plus Jakarta Sans"},"layout":"story-first","cover_style":"torii","sections":["cover","couple","story","events","gallery","rsvp"],"media":{"cover":"/index/photo-1519741497674-611481863552.jpg","gallery":["/index/photo-1465495976277-4387d4b0b4c6.jpg"]}}',
 '["RSVP","Cerita pasangan","Galeri foto"]', 2),
('S03', 'Minimalist Romance', 'minimalist', 'Putih bersih, tipografi elegan, ruang lega.', '/index/photo-1465495976277-4387d4b0b4c6.jpg', 1,
 '{"palette":{"background":"#202020","surface":"#ffffff","accent":"#a8a29e","text":"#f7f5f3"},"typography":{"heading":"Cormorant Garamond","body":"Plus Jakarta Sans"},"layout":"editorial","cover_style":"split","sections":["cover","couple","events","gallery","rsvp"],"media":{"cover":"/index/photo-1465495976277-4387d4b0b4c6.jpg","gallery":["/index/photo-1511285560929-80b456fea0bc.jpg"]}}',
 '["RSVP","Galeri foto"]', 3),
('G01', 'Golden Kirana', 'luxury', 'Resmi dan megah dengan sentuhan emas premium.', '/index/photo-1519741497674-611481863552.jpg', 1,
 '{"palette":{"background":"#1f1a17","surface":"#f3ead9","accent":"#c9a27c","text":"#fffaf1"},"typography":{"heading":"Alex Brush","body":"Plus Jakarta Sans"},"layout":"classic","cover_style":"fullbleed","sections":["cover","couple","story","events","gallery","rsvp","gift"],"media":{"cover":"/index/photo-1519741497674-611481863552.jpg","gallery":["/index/photo-1465495976277-4387d4b0b4c6.jpg"]}}',
 '["RSVP","Cerita pasangan","Galeri foto","Amplop digital"]', 4),
('G02', 'Sakura Premium', 'japanese', 'Taman sakura versi premium dengan layout ganda.', '/index/photo-1511285560929-80b456fea0bc.jpg', 1,
 '{"palette":{"background":"#241c1c","surface":"#faf3ea","accent":"#d8a0a0","text":"#fff8f4"},"typography":{"heading":"Cormorant Garamond","body":"Plus Jakarta Sans"},"layout":"story-first","cover_style":"parallax","sections":["cover","couple","story","events","gallery","rsvp","gift"],"media":{"cover":"/index/photo-1511285560929-80b456fea0bc.jpg","gallery":["/index/photo-1519741497674-611481863552.jpg"]}}',
 '["RSVP","Cerita pasangan","Galeri foto","Amplop digital"]', 5);

INSERT INTO template_packages (template_id, package_id) VALUES
('S01', 'silver'), ('S02', 'silver'), ('S03', 'silver'),
('G01', 'gold'), ('G02', 'gold');

-- +goose Down
DELETE FROM template_packages WHERE template_id IN ('S01', 'S02', 'S03', 'G01', 'G02');
DELETE FROM templates WHERE id IN ('S01', 'S02', 'S03', 'G01', 'G02');
DELETE FROM package_features;
DELETE FROM packages;
