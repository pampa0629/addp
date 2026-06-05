package migrations

import "embed"

// FS contains Meta module SQL migrations.
//
//go:embed *.sql
var FS embed.FS
