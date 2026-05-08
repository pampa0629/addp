package dbbridge

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
)

type sampleCatalogProvider struct {
	namespaces []plugin.CatalogNode
	items      map[string][]plugin.CatalogNode
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

func (p *sampleCatalogProvider) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	if len(parent.Segments) == 0 {
		return p.namespaces, nil
	}
	return p.items[parent.Segments[0].Name], nil
}

func (p *sampleCatalogProvider) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return nil, nil
}

func TestGenerateCatalogSampleQueryPrefersTableWithRows(t *testing.T) {
	cp := &sampleCatalogProvider{
		namespaces: []plugin.CatalogNode{
			namespaceNode("public"),
		},
		items: map[string][]plugin.CatalogNode{
			"public": {
				itemNode("empty_table", 0),
				itemNode("cities", 12),
			},
		},
	}

	query, ok := generateCatalogSampleQuery(context.Background(), cp, nil, 1, "postgresql")
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
		namespaces: []plugin.CatalogNode{
			namespaceNode("analytics"),
		},
		items: map[string][]plugin.CatalogNode{
			"analytics": {
				itemNode("events", 0),
			},
		},
	}

	query, ok := generateCatalogSampleQuery(context.Background(), cp, nil, 1, "mysql")
	if !ok {
		t.Fatal("expected catalog sample query")
	}

	const want = "SELECT *\nFROM `analytics`.`events`\nLIMIT 10"
	if query != want {
		t.Fatalf("unexpected query:\nwant: %s\ngot:  %s", want, query)
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

func namespaceNode(name string) plugin.CatalogNode {
	return plugin.CatalogNode{
		Name:        name,
		Path:        plugin.CatalogPath{Segments: []plugin.CatalogSegment{{Name: name}}},
		IsContainer: true,
	}
}

func itemNode(name string, rowCount int64) plugin.CatalogNode {
	return plugin.CatalogNode{
		Name:   name,
		IsItem: true,
		Stats: map[string]interface{}{
			"row_count": rowCount,
		},
	}
}
