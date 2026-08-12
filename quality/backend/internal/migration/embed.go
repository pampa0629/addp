package migration

import "embed"

// EmbeddedSQL contains the immutable Quality forward migrations.
//
//go:embed sql/*.up.sql
var EmbeddedSQL embed.FS
