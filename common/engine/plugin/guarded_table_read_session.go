package plugin

import (
	"context"

	"github.com/addp/common/datatype"
)

// GuardedTableReadAuthorizer checks the locked source and its output fields.
// Fields are the columns of the actual generated read, including selected
// spatial outputs. The callback may apply the caller's live schema checks.
type GuardedTableReadAuthorizer func(context.Context, *QueryReadSet, []datatype.FieldInfo) error

// GuardedTableReadSessionProvider proves the actual locked source and calls
// authorize before querying its content. A successful session carries the
// same read set. Ordinary table readers need not support this contract.
type GuardedTableReadSessionProvider interface {
	TableReadSessionProvider
	OpenGuardedTableReadSession(ctx context.Context, connInfo ConnectionInfo, path EngineCatalogPath,
		opts TableReadSessionOptions, authorize GuardedTableReadAuthorizer) (TableReadSession, error)
}
