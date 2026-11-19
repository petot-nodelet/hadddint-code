-- name: CreateOrder :one
INSERT INTO orders (user_id, package_id, template_id)
VALUES (?, ?, ?)
RETURNING id;

-- name: GetOrderByIDForUser :one
SELECT o.id, o.package_id, p.name AS package_name,
       o.template_id, t.name AS template_name, o.status
FROM orders AS o
JOIN packages AS p ON p.id = o.package_id
JOIN templates AS t ON t.id = o.template_id
WHERE o.id = ? AND o.user_id = ?;
