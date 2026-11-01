package plugin

import "context"

// PreparedFederatedQuery binds remote native read facts and execution to the
// same short-lived Runtime session. Closing it releases that session, not a task.
type PreparedFederatedQuery interface {
	QueryReadPlan
	SourceCatalogModels() map[uint]EngineCatalogModelSpec
	Execute(context.Context) (*QueryResult, error)
	Close(context.Context) error
}

// PreparedFederatedQueryProvider is optional: runtimes without complete native
// read facts retain the unresolved protection gate, never an inferred read set.
type PreparedFederatedQueryProvider interface {
	PrepareFederatedQuery(context.Context, ConnectionInfo, FederatedQueryRequest) (PreparedFederatedQuery, error)
}
