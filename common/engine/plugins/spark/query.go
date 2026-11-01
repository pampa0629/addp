package spark

import (
	"context"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/federatedquery"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonquery "github.com/addp/common/query"
)

func (*Plugin) QueryLanguages() []string { return []string{"sql"} }

func (*Plugin) AnalyzeFederatedQuery(_ context.Context, request plugin.FederatedQueryRequest) (*plugin.QueryAnalysis, error) {
	if request.Language != "sql" || strings.TrimSpace(request.Query) == "" {
		return nil, commoni18n.NewMessageError("engine.spark.query_invalid")
	}
	if err := commonquery.RequireReadOnly(request.Query); err != nil {
		return nil, err
	}
	// Discovery is not a read-set proof. The native Driver performs the complete
	// Catalyst check before resolution or execution, against its authorized mounts.
	return plugin.NewQueryAnalysis("sql", plugin.QuerySchemaCoverageUnknown)
}

func (p *Plugin) ResolveSourceEngineIDs(query string, candidates []plugin.FederatedQuerySource) []uint {
	return federatedquery.ReferencedSourceEngineIDs(query, candidates, p.Capabilities().Compute.Query.Federation.SourceEngineTypes)
}

func (*Plugin) ResolveObjectTableReferences(string, []plugin.FederatedQuerySource) []plugin.FederatedQueryObjectTableReference {
	return nil
}

func (*Plugin) ExecuteFederatedQuery(ctx context.Context, runtimeConn plugin.ConnectionInfo, request plugin.FederatedQueryRequest) (*plugin.QueryResult, error) {
	if request.TargetEngineID == 0 {
		return nil, commoni18n.NewMessageError("engine.spark.query_invalid")
	}
	return plugin.HTTPExecuteFederatedQuery(ctx, runtimeConn, request)
}
