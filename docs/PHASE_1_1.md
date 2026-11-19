# SUKAMOTO Phase 1.1 — SQLite foundation

The Go application lives in `app/`. The original static HTML pages and their
assets remain in the repository as design references.

## Requirements

- Go 1.26 or newer
- Node.js and npm for the Tailwind CSS CLI build

## Run locally

```sh
cd app
npm ci
npm run css:build
go run ./cmd/sukamoto
```

The application listens on `:8080` by default, creates `data/sukamoto.db`,
enables SQLite WAL, foreign keys, and a 5-second busy timeout, then runs Goose
migrations and seed data automatically.

Configuration:

| Variable | Default | Description |
|---|---|---|
| `SUKAMOTO_ADDR` | `:8080` | HTTP listen address |
| `DATABASE_PATH` | `data/sukamoto.db` | Persistent SQLite database file |
| `APP_ENV` | `development` | Use `production` to enable Secure session cookies |
| `STATIC_DIR` | `..` from `app/` | Repository root containing the original design images |

No PostgreSQL service or `DATABASE_URL` is used.

## Database and SQLC

- Goose migrations: `app/db/migrations/`
- SQLC source queries: `app/db/query/`
- Generated query layer: `app/db/generated/`
- SQLC configuration: `app/sqlc.yaml`

Regenerate typed database methods with `sqlc generate` from `app/`. Add schema
changes as a new Goose migration rather than editing migrations already applied
to a deployed database.

## Backup and restore

The database utility creates consistent SQLite backups with `VACUUM INTO`.
Restore always writes to a new destination; it refuses to overwrite an existing
file. Stop the application before restoring the database it will use.

```sh
cd app
go run ./cmd/sukamoto-db backup data/sukamoto.db data/backups/sukamoto.db
go run ./cmd/sukamoto-db restore data/backups/sukamoto.db data/restored/sukamoto.db
```

After restoring, point `DATABASE_PATH` at the restored file.

## Authentication

Customer registration creates a `customer` account and logs it in. Passwords
use bcrypt. SCS sessions and their SHA-256 token hashes are persisted in SQLite.
The session cookie is HttpOnly, SameSite=Lax, and Secure in production. Every
POST is protected by a per-session CSRF token. Admin access is role-checked
server-side; there is no default or hard-coded admin account.

## Tests and generated assets

```sh
cd app
go test ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/sukamoto-arm64 ./cmd/sukamoto
npm run css:build
```

The SQLite driver is `modernc.org/sqlite`, a pure-Go implementation; the ARM64
build does not require CGO.
