package migrations

import "embed"

// SearchFS contains the versioned search continuation schema transition.
//
//go:embed 047_search_continuation.sql
var SearchFS embed.FS
