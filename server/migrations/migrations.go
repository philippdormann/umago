// Package migrations holds the SQL schema migrations, embedded into the binary.
package migrations

import "embed"

// FS contains the single-schema migration per dialect, embedded into the binary.
// database.Migrate picks the sqlite or postgres directory and records bare
// filenames in schema_migrations.
//
//go:embed sqlite/*.sql postgres/*.sql
var FS embed.FS
