package plugin

import "context"

// QueryReadAuthorizer observes current source authority for the actual locked
// query sources. It must run before the first business-content read.
type QueryReadAuthorizer func(context.Context, *QueryReadSet) error

type GuardedQueryReadSession interface {
	QueryReadSession
	ReadSessionEvidence
}

// GuardedQueryReadSessionProvider admits only its proven native subset. It
// consumes the same immutable prepared query once and releases all resources
// when proof or authorization fails, without falling back to ordinary Execute.
type GuardedQueryReadSessionProvider interface {
	QueryRuntimeProvider
	OpenGuardedQueryReadSession(context.Context, PreparedQuery, QueryReadAuthorizer) (GuardedQueryReadSession, error)
}
