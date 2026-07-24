package migration

import "embed"

// EmbeddedSQL contains the immutable IAM forward migrations shipped with System.
//
//go:embed sql/*.up.sql
var EmbeddedSQL embed.FS
