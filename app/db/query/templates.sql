-- name: ListTemplates :many
WITH sort_args AS (
    SELECT CAST(sqlc.arg(sort_by) AS TEXT) AS sort_by
)
SELECT t.id, t.name, t.category, t.description, t.thumbnail, t.is_active,
       t.config, t.features, t.sort_order
FROM templates AS t
CROSS JOIN sort_args
WHERE t.is_active = 1
  AND (CAST(@package_id AS TEXT) = '' OR EXISTS (
      SELECT 1 FROM template_packages AS tp
      WHERE tp.template_id = t.id AND tp.package_id = CAST(@package_id AS TEXT)
  ))
  AND (CAST(@category AS TEXT) = '' OR t.category = CAST(@category AS TEXT))
  AND (CAST(@search AS TEXT) = '' OR lower(t.name) LIKE '%' || lower(CAST(@search AS TEXT)) || '%'
       OR lower(t.description) LIKE '%' || lower(CAST(@search AS TEXT)) || '%')
ORDER BY
  CASE WHEN sort_args.sort_by = 'name_asc' THEN lower(t.name) END ASC,
  CASE WHEN sort_args.sort_by = 'name_desc' THEN lower(t.name) END DESC,
  CASE WHEN sort_args.sort_by = 'category' THEN lower(t.category) END ASC,
  t.sort_order, t.id
LIMIT @page_size OFFSET @page_offset;

-- name: CountTemplates :one
SELECT count(*)
FROM templates AS t
WHERE t.is_active = 1
  AND (CAST(@package_id AS TEXT) = '' OR EXISTS (
      SELECT 1 FROM template_packages AS tp
      WHERE tp.template_id = t.id AND tp.package_id = CAST(@package_id AS TEXT)
  ))
  AND (CAST(@category AS TEXT) = '' OR t.category = CAST(@category AS TEXT))
  AND (CAST(@search AS TEXT) = '' OR lower(t.name) LIKE '%' || lower(CAST(@search AS TEXT)) || '%'
       OR lower(t.description) LIKE '%' || lower(CAST(@search AS TEXT)) || '%');

-- name: ListTemplateCategories :many
SELECT DISTINCT category
FROM templates
WHERE is_active = 1
ORDER BY category;

-- name: GetTemplate :one
SELECT id, name, category, description, thumbnail, is_active, config, features, sort_order
FROM templates
WHERE id = ? AND is_active = 1;

-- name: GetTemplatePackages :many
SELECT p.id, p.name, p.available, p.coming_soon
FROM template_packages AS tp
JOIN packages AS p ON p.id = tp.package_id
WHERE tp.template_id = ?
ORDER BY p.sort_order;

-- name: ListTemplatePackages :many
SELECT tp.template_id, p.id, p.name, p.available, p.coming_soon
FROM template_packages AS tp
JOIN packages AS p ON p.id = tp.package_id
JOIN templates AS t ON t.id = tp.template_id
WHERE t.is_active = 1
ORDER BY p.sort_order;

-- name: ListPackages :many
SELECT id, name, tagline, description, price_cents, duration_days,
       max_photos, max_videos, max_events, coming_soon, available
FROM packages
ORDER BY sort_order;

-- name: GetPackage :one
SELECT id, name, tagline, description, price_cents, duration_days,
       max_photos, max_videos, max_events, coming_soon, available
FROM packages
WHERE id = ?;

-- name: TemplateSupportsPackage :one
SELECT EXISTS (
    SELECT 1 FROM template_packages
    WHERE template_id = ? AND package_id = ?
) AS supported;
