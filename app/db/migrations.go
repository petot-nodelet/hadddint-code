package db

import "embed"

// Migrations contains the SQL files applied when the application opens SQLite.
//
//go:embed migrations/*.sql
var Migrations embed.FS
