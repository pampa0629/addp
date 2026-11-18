package migrations

import "embed"

// SearchFS contains the versioned search continuation schema transition.
//
//go:embed 047_search_continuation.sql
var SearchFS embed.FS

// PreviewStateUserScopeSQL resets shared preferences and installs user ownership.
//
//go:embed 048_scope_preview_state_to_user.sql
var PreviewStateUserScopeSQL string
