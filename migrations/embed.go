package migrations

import "embed"

// FS contains immutable, forward-only database migrations.
//
//go:embed *.sql
var FS embed.FS
