package migrations

import "embed"

// SearchFS contains the versioned Manager search schema transitions.
//
//go:embed 047_search_continuation.sql 049_scope_search_history_to_tenant.sql 050_keyword_search.sql 051_vector_search_revision.sql
var SearchFS embed.FS

// PreviewStateUserScopeSQL resets shared preferences and installs user ownership.
//
//go:embed 048_scope_preview_state_to_user.sql
var PreviewStateUserScopeSQL string
