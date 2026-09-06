package shared

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestMySQLCompatibleSampleQueryEnumeratesCurrentCatalogFields(t *testing.T) {
	path := plugin.EngineCatalogPath{
		Version:  plugin.EngineCatalogPathVersion,
		EngineID: 3,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
			{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "business"},
			{Term: plugin.EngineCatalogTermTable, Kind: plugin.EngineCatalogKindTable, Name: "customers"},
		},
	}
	provenance := MySQLCompatibleQueryProvenance{
		DescribeFacts: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
			return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{
				{Name: "id"}, {Name: "email"}, {Name: "odd`name"},
			}}}, nil
		},
	}

	query := provenance.GenerateSampleQuery(context.Background(), nil, path, "mysql", 10)
	want := "SELECT `id`, `email`, `odd``name` FROM `business`.`customers` LIMIT 10"
	if query != want {
		t.Fatalf("GenerateSampleQuery() = %q, want %q", query, want)
	}
}

func TestMySQLCompatibleSampleQueryFailsClosedWithoutCurrentFields(t *testing.T) {
	provenance := MySQLCompatibleQueryProvenance{}
	if query := provenance.GenerateSampleQuery(context.Background(), nil, plugin.EngineCatalogPath{}, "mysql", 10); query != "" {
		t.Fatalf("GenerateSampleQuery() = %q, want empty", query)
	}
}
