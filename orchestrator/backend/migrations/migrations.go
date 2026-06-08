package migrations

import "embed"

// FS contains Orchestrator module SQL migrations.
//
//go:embed *.sql
var FS embed.FS
