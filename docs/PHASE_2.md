# SUKAMOTO Phase 2 — Template catalog and preview

The template catalog is rendered server-side with Templ and reads active
templates, package support, categories, and preview configuration from SQLite.
`GET /templates` supports package/category filters, case-insensitive search,
four stable sort modes, and nine-item pagination. `GET /templates/{id}` shows
the template's preview, supported packages, and included features.

Template preview uses a shared renderer with a common sample invitation.
Palette colors, typography, layout, cover style, sections, and local media are
loaded from each template's JSON configuration. The renderer validates color,
font, layout, section, and media values before using them in HTML/CSS.

Selecting a template checks login state, customer role, active template,
package availability, and the template/package relationship. Anonymous users
are sent through login or registration with their choice preserved. A
successful selection creates a SQLite draft order. SQLite additionally enforces
the selected template/package pair with a composite foreign key.

Template administration remains a Phase 5 UI; the database schema already
stores activation state, category, thumbnail, JSON renderer configuration,
features, sort order, and supported packages.
