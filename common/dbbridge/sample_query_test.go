package dbbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type sampleCatalogProvider struct {
	namespaces []plugin.EngineCatalogEntry
	items      map[string][]plugin.EngineCatalogEntry
	parents    []plugin.EngineCatalogPath
	model      plugin.EngineCatalogModelSpec
}

func (p *sampleCatalogProvider) Type() string { return "sample" }

func (p *sampleCatalogProvider) DisplayName() string { return "Sample" }

func (p *sampleCatalogProvider) EngineOrigin() string { return "general" }

func (p *sampleCatalogProvider) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *sampleCatalogProvider) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *sampleCatalogProvider) DefaultPort() int { return 0 }

func (p *sampleCatalogProvider) RequiredFields() []string { return nil }

func (p *sampleCatalogProvider) SensitiveFields() []string          { return nil }
func (p *sampleCatalogProvider) ConnectionIdentityFields() []string { return []string{"host"} }

func (p *sampleCatalogProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}

func (p *sampleCatalogProvider) QueryLanguages() []string { return []string{"sql"} }
func (p *sampleCatalogProvider) GenerateSampleQuery(context.Context, plugin.ConnectionInfo, plugin.SampleQueryOptions) (string, string) {
	return "", "sql"
}
func (p *sampleCatalogProvider) PrepareQuery(context.Context, plugin.ConnectionInfo, plugin.QueryRequest) (plugin.PreparedQuery, error) {
	return nil, errors.New("not implemented")
}
func (p *sampleCatalogProvider) SQLDialect() string { return "postgresql" }
func (p *sampleCatalogProvider) ExecuteSQL(context.Context, plugin.ConnectionInfo, string, plugin.QueryOptions) (*plugin.QueryResult, error) {
	return nil, errors.New("not implemented")
}

func (p *sampleCatalogProvider) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	if p.model.PathVersion != "" {
		return p.model
	}
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
}

func (p *sampleCatalogProvider) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.parents = append(p.parents, parent)
	if plugin.IsEngineCatalogRootPath(parent) {
		return p.namespaces, nil
	}
	business := plugin.EngineCatalogPathWithoutRoot(parent)
	return p.items[business.Segments[0].Name], nil
}

func (p *sampleCatalogProvider) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, nil
}

func TestGenerateCatalogSampleQueryPrefersTableWithRows(t *testing.T) {
	cp := &sampleCatalogProvider{
		namespaces: []plugin.EngineCatalogEntry{
			namespaceNode("public"),
		},
		items: map[string][]plugin.EngineCatalogEntry{
			"public": {
				itemNode("empty_table", 0),
				itemNode("cities", 12),
			},
		},
	}

	query, err := generateCatalogSampleQuery(context.Background(), cp, cp, nil, 1, "postgresql", 10)
	if err != nil {
		t.Fatalf("expected catalog sample query: %v", err)
	}

	const want = "SELECT *\nFROM \"public\".\"cities\"\nLIMIT 10"
	if query != want {
		t.Fatalf("unexpected query:\nwant: %s\ngot:  %s", want, query)
	}
}

func TestGenerateCatalogSampleQueryRejectsEmptyTables(t *testing.T) {
	cp := &sampleCatalogProvider{
		namespaces: []plugin.EngineCatalogEntry{
			namespaceNode("analytics"),
		},
		items: map[string][]plugin.EngineCatalogEntry{
			"analytics": {
				itemNode("events", 0),
			},
		},
	}

	query, err := generateCatalogSampleQuery(context.Background(), cp, cp, nil, 1, "mysql", 10)
	if !errors.Is(err, ErrSampleQueryUnavailable) || query != "" {
		t.Fatalf("generateCatalogSampleQuery() = (%q, %v), want unavailable", query, err)
	}
}

func TestGenerateCatalogSampleQueryRequiresTableLeafModel(t *testing.T) {
	cp := &sampleCatalogProvider{
		model: plugin.ObjectCatalogModel(),
		namespaces: []plugin.EngineCatalogEntry{
			namespaceNode("bucket"),
		},
		items: map[string][]plugin.EngineCatalogEntry{
			"bucket": {
				itemNode("object.csv", 12),
			},
		},
	}

	query, err := generateCatalogSampleQuery(context.Background(), cp, cp, nil, 1, "postgresql", 10)
	if !errors.Is(err, ErrSampleQueryUnavailable) || query != "" {
		t.Fatalf("generateCatalogSampleQuery() = (%q, %v), want unavailable for non-table catalog", query, err)
	}
}

func TestTableSampleSQLEscapesIdentifiers(t *testing.T) {
	tests := []struct {
		name       string
		engineType string
		namespace  string
		table      string
		want       string
	}{
		{
			name:       "postgresql double quotes",
			engineType: "postgresql",
			namespace:  `data"mart`,
			table:      `city"pop`,
			want:       `SELECT *` + "\n" + `FROM "data""mart"."city""pop"` + "\n" + `LIMIT 10`,
		},
		{
			name:       "mysql backticks",
			engineType: "mysql",
			namespace:  "data`mart",
			table:      "city`pop",
			want:       "SELECT *\nFROM `data``mart`.`city``pop`\nLIMIT 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tableSampleSQL(tt.engineType, tt.namespace, tt.table, 10); got != tt.want {
				t.Fatalf("unexpected query:\nwant: %s\ngot:  %s", tt.want, got)
			}
		})
	}
}

func TestTableSampleSQLCanReturnUnboundedBaseQuery(t *testing.T) {
	got := tableSampleSQL("postgresql", "public", "orders", 0)
	const want = "SELECT *\nFROM \"public\".\"orders\""
	if got != want {
		t.Fatalf("unexpected query:\nwant: %s\ngot:  %s", want, got)
	}
}

func TestListCatalogChildrenEmptyPathReturnsExplicitRoot(t *testing.T) {
	cp := &sampleCatalogProvider{}
	plugin.Register(cp)
	t.Cleanup(func() {
		plugin.Unregister(cp.Type())
	})

	nodes, err := ListEngineCatalogChildren(context.Background(), &models.Engine{
		ID:         99,
		Name:       "Analytics DB",
		EngineType: cp.Type(),
	}, plugin.EngineCatalogPath{}, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListEngineCatalogChildren() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %#v, want single root", nodes)
	}
	root := nodes[0]
	if root.Name != "Analytics DB" || root.Term != plugin.EngineCatalogTermServer || root.Kind != plugin.EngineCatalogTermServer || root.Role != plugin.EngineCatalogRoleBranch {
		t.Fatalf("root = %#v", root)
	}
	if !plugin.IsEngineCatalogRootPath(root.Path) {
		t.Fatalf("root path = %#v, want explicit catalog root", root.Path)
	}
	if len(cp.parents) != 0 {
		t.Fatalf("provider was called with parents %#v", cp.parents)
	}
}

func TestListCatalogChildrenExplicitRootForwardsToProvider(t *testing.T) {
	cp := &sampleCatalogProvider{
		namespaces: []plugin.EngineCatalogEntry{
			namespaceNode("public"),
		},
	}
	plugin.Register(cp)
	t.Cleanup(func() {
		plugin.Unregister(cp.Type())
	})

	rootPath := plugin.EngineCatalogRootPath(cp.EngineCatalogModel(), 99)
	nodes, err := ListEngineCatalogChildren(context.Background(), &models.Engine{
		ID:         99,
		Name:       "Analytics DB",
		EngineType: cp.Type(),
	}, rootPath, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListEngineCatalogChildren(root) error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "public" {
		t.Fatalf("nodes = %#v, want public namespace", nodes)
	}
	if len(cp.parents) != 1 || !plugin.IsEngineCatalogRootPath(cp.parents[0]) {
		t.Fatalf("provider parents = %#v, want explicit root", cp.parents)
	}
}

func namespaceNode(name string) plugin.EngineCatalogEntry {
	return plugin.EngineCatalogEntry{
		Name: name,
		Path: plugin.TabularNamespacePath(1, plugin.EngineCatalogTermSchema, name),
		Role: plugin.EngineCatalogRoleBranch,
	}
}

func itemNode(name string, rowCount int64) plugin.EngineCatalogEntry {
	return plugin.EngineCatalogEntry{
		Name:  name,
		Role:  plugin.EngineCatalogRoleLeaf,
		Table: &datatype.TableInfo{RowCount: &rowCount},
	}
}
