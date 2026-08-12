package duckdb

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/federatedquery"
)

const RuntimeAPI = "addp.query-runtime/v1"

type Plugin struct{}

var qualifiedSourcePattern = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)(?:\.([a-zA-Z_][a-zA-Z0-9_]*))?\b`)

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

func (p *Plugin) ResolveSourceEngineIDs(query string, candidates []plugin.FederatedQuerySource) []uint {
	referenced := make(map[string]struct{})
	for _, match := range qualifiedSourcePattern.FindAllStringSubmatch(query, -1) {
		if len(match) > 1 {
			referenced[match[1]] = struct{}{}
		}
	}
	supported := make(map[string]struct{})
	for _, engineType := range p.Capabilities().Compute.Query.Federation.SourceEngineTypes {
		supported[engineType] = struct{}{}
	}
	ids := make([]uint, 0, len(referenced))
	seen := make(map[uint]struct{})
	for _, candidate := range candidates {
		if candidate.ID == 0 || candidate.LifecycleState != "active" {
			continue
		}
		if _, ok := supported[candidate.EngineType]; !ok {
			continue
		}
		_, byName := referenced[candidate.Name]
		_, byAlias := referenced[federatedquery.SanitizeIdentifier(candidate.Name)]
		if !byName && !byAlias {
			continue
		}
		if _, duplicate := seen[candidate.ID]; duplicate {
			continue
		}
		seen[candidate.ID] = struct{}{}
		ids = append(ids, candidate.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
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
	for _, match := range qualifiedSourcePattern.FindAllStringSubmatch(query, -1) {
		if len(match) != 4 {
			continue
		}
		if _, sourceExists := sourceNames[match[1]]; !sourceExists {
			continue
		}
		tableName := match[2]
		if match[3] != "" {
			tableName += "." + match[3]
		}
		key := match[1] + "\x00" + tableName
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		references = append(references, plugin.FederatedQueryObjectTableReference{
			SourceName: match[1],
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
