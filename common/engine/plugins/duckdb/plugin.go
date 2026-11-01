package duckdb

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/federatedquery"
	commonquery "github.com/addp/common/query"
)

const RuntimeAPI = "addp.query-runtime/v1"

type Plugin struct{}

func init() {
	plugin.Register(&Plugin{})
}

func (p *Plugin) Type() string              { return "duckdb" }
func (p *Plugin) DisplayName() string       { return "DuckDB 联邦查询 Runtime" }
func (p *Plugin) EngineOrigin() string      { return "extension" }
func (p *Plugin) DefaultPort() int          { return 8104 }
func (p *Plugin) RequiredFields() []string  { return []string{"host", "port"} }
func (p *Plugin) SensitiveFields() []string { return nil }
func (p *Plugin) ConnectionIdentityFields() []string {
	return []string{"protocol", "host", "port"}
}

func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewFederatedQueryCapabilities(
		p.Type(), RuntimeAPI,
		[]string{"postgresql", "mysql", "minio", "s3"},
		[]string{"parquet"},
	)
}

func (p *Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *Plugin) QueryLanguages() []string { return []string{"sql"} }

func (p *Plugin) AnalyzeFederatedQuery(_ context.Context, req plugin.FederatedQueryRequest) (*plugin.QueryAnalysis, error) {
	if !strings.EqualFold(strings.TrimSpace(req.Language), "sql") || strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("DuckDB federated query requires non-empty SQL")
	}
	if err := commonquery.RequireReadOnly(req.Query); err != nil {
		return nil, fmt.Errorf("DuckDB federated query must be read-only: %w", err)
	}
	return plugin.NewQueryAnalysis("sql", plugin.QuerySchemaCoverageUnknown)
}

func (p *Plugin) ResolveSourceEngineIDs(query string, candidates []plugin.FederatedQuerySource) []uint {
	return federatedquery.ReferencedSourceEngineIDs(query, candidates, p.Capabilities().Compute.Query.Federation.SourceEngineTypes)
}

func (p *Plugin) ResolveObjectTableReferences(
	query string,
	candidates []plugin.FederatedQuerySource,
) []plugin.FederatedQueryObjectTableReference {
	references := make([]plugin.FederatedQueryObjectTableReference, 0)
	seen := make(map[string]struct{})
	sourceNames := make(map[string]struct{}, len(candidates)*2)
	for _, candidate := range candidates {
		if candidate.ID == 0 || candidate.LifecycleState != "active" {
			continue
		}
		sourceNames[candidate.Name] = struct{}{}
		sourceNames[federatedquery.SanitizeIdentifier(candidate.Name)] = struct{}{}
	}
	for _, match := range federatedquery.QualifiedReferences(query) {
		if len(match) < 2 || len(match) > 3 {
			continue
		}
		if _, sourceExists := sourceNames[match[0]]; !sourceExists {
			continue
		}
		tableName := strings.Join(match[1:], ".")
		key := match[0] + "\x00" + tableName
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		references = append(references, plugin.FederatedQueryObjectTableReference{
			SourceName: match[0],
			TableName:  tableName,
		})
	}
	return references
}

func (p *Plugin) ExecuteFederatedQuery(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	req plugin.FederatedQueryRequest,
) (*plugin.QueryResult, error) {
	return plugin.HTTPExecuteFederatedQuery(ctx, connInfo, req)
}

func (p *Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	baseURL, err := plugin.RuntimeBaseURL(connInfo)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to DuckDB runtime: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DuckDB runtime health check returned HTTP %d", resp.StatusCode)
	}
	return nil
}
