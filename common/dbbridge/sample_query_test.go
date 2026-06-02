package dbbridge

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type sampleCatalogProvider struct {
	namespaces []plugin.CatalogEntry
	items      map[string][]plugin.CatalogEntry
	parents    []plugin.CatalogPath
	model      plugin.CatalogModelSpec
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

func (p *sampleCatalogProvider) SensitiveFields() []string { return nil }

func (p *sampleCatalogProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}

func (p *sampleCatalogProvider) CatalogModel() plugin.CatalogModelSpec {
	if p.model.PathVersion != "" {
		return p.model
	}
	return plugin.TabularCatalogModel(plugin.CatalogTermSchema)
}

func (p *sampleCatalogProvider) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	p.parents = append(p.parents, parent)
	if plugin.IsCatalogRootPath(parent) {
		return p.namespaces, nil
	}
	business := plugin.CatalogPathWithoutRoot(parent)
	return p.items[business.Segments[0].Name], nil
}

func (p *sampleCatalogProvider) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return nil, nil
}

func TestGenerateCatalogSampleQueryPrefersTableWithRows(t *testing.T) {
	cp := &sampleCatalogProvider{
		namespaces: []plugin.CatalogEntry{
			namespaceNode("public"),
		},
		items: map[string][]plugin.CatalogEntry{
			"public": {
				itemNode("empty_table", 0),
				itemNode("cities", 12),
			},
		},
	}

	query, ok := generateCatalogSampleQuery(context.Background(), cp, cp, nil, 1, "postgresql")
	if !ok {
		t.Fatal("expected catalog sample query")
	}

	const want = "SELECT *\nFROM \"public\".\"cities\"\nLIMIT 10"
	if query != want {
		t.Fatalf("unexpected query:\nwant: %s\ngot:  %s", want, query)
	}
}

func TestGenerateCatalogSampleQueryFallsBackToFirstItem(t *testing.T) {
	cp := &sampleCatalogProvider{
		namespaces: []plugin.CatalogEntry{
			namespaceNode("analytics"),
		},
		items: map[string][]plugin.CatalogEntry{
			"analytics": {
				itemNode("events", 0),
			},
		},
	}

	query, ok := generateCatalogSampleQuery(context.Background(), cp, cp, nil, 1, "mysql")
	if !ok {
		t.Fatal("expected catalog sample query")
	}

	const want = "SELECT *\nFROM `analytics`.`events`\nLIMIT 10"
	if query != want {
		t.Fatalf("unexpected query:\nwant: %s\ngot:  %s", want, query)
	}
}

func TestGenerateCatalogSampleQueryRequiresTableLeafModel(t *testing.T) {
	cp := &sampleCatalogProvider{
		model: plugin.ObjectCatalogModel(),
		namespaces: []plugin.CatalogEntry{
			namespaceNode("bucket"),
		},
		items: map[string][]plugin.CatalogEntry{
			"bucket": {
				itemNode("object.csv", 12),
			},
		},
	}

	query, ok := generateCatalogSampleQuery(context.Background(), cp, cp, nil, 1, "postgresql")
	if ok || query != "" {
		t.Fatalf("generateCatalogSampleQuery() = (%q, %v), want no SQL sample for non-table catalog", query, ok)
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
			if got := tableSampleSQL(tt.engineType, tt.namespace, tt.table); got != tt.want {
				t.Fatalf("unexpected query:\nwant: %s\ngot:  %s", tt.want, got)
			}
		})
	}
}

func TestListCatalogChildrenEmptyPathReturnsExplicitRoot(t *testing.T) {
	cp := &sampleCatalogProvider{}
	plugin.Register(cp)
	t.Cleanup(func() {
		plugin.Unregister(cp.Type())
	})

	nodes, err := ListCatalogChildren(context.Background(), &models.Engine{
		ID:         99,
		Name:       "Analytics DB",
		EngineType: cp.Type(),
	}, plugin.CatalogPath{}, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListCatalogChildren() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %#v, want single root", nodes)
	}
	root := nodes[0]
	if root.Name != "Analytics DB" || root.Term != plugin.CatalogTermServer || root.Kind != plugin.CatalogTermServer || root.Role != plugin.CatalogRoleBranch {
		t.Fatalf("root = %#v", root)
	}
	if !plugin.IsCatalogRootPath(root.Path) {
		t.Fatalf("root path = %#v, want explicit catalog root", root.Path)
	}
	if len(cp.parents) != 0 {
		t.Fatalf("provider was called with parents %#v", cp.parents)
	}
}

func TestListCatalogChildrenExplicitRootForwardsToProvider(t *testing.T) {
	cp := &sampleCatalogProvider{
		namespaces: []plugin.CatalogEntry{
			namespaceNode("public"),
		},
	}
	plugin.Register(cp)
	t.Cleanup(func() {
		plugin.Unregister(cp.Type())
	})

	rootPath := plugin.CatalogRootPath(cp.CatalogModel(), 99)
	nodes, err := ListCatalogChildren(context.Background(), &models.Engine{
		ID:         99,
		Name:       "Analytics DB",
		EngineType: cp.Type(),
	}, rootPath, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListCatalogChildren(root) error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "public" {
		t.Fatalf("nodes = %#v, want public namespace", nodes)
	}
	if len(cp.parents) != 1 || !plugin.IsCatalogRootPath(cp.parents[0]) {
		t.Fatalf("provider parents = %#v, want explicit root", cp.parents)
	}
}

func namespaceNode(name string) plugin.CatalogEntry {
	return plugin.CatalogEntry{
		Name: name,
		Path: plugin.TabularNamespacePath(1, plugin.CatalogTermSchema, name),
		Role: plugin.CatalogRoleBranch,
	}
}

func itemNode(name string, rowCount int64) plugin.CatalogEntry {
	return plugin.CatalogEntry{
		Name:  name,
		Role:  plugin.CatalogRoleLeaf,
		Table: &datatype.TableInfo{RowCount: &rowCount},
	}
}
