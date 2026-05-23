package preview

import (
	"context"
	"github.com/addp/common/datatype"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/manager/internal/models"
)

type recordingCatalogProvider struct {
	parent plugin.CatalogPath
}

func (p *recordingCatalogProvider) Type() string         { return "recording" }
func (p *recordingCatalogProvider) DisplayName() string  { return "recording" }
func (p *recordingCatalogProvider) Description() string  { return "" }
func (p *recordingCatalogProvider) Version() string      { return "" }
func (p *recordingCatalogProvider) EngineOrigin() string { return "general" }
func (p *recordingCatalogProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingCatalogProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingCatalogProvider) DefaultPort() int          { return 0 }
func (p *recordingCatalogProvider) RequiredFields() []string  { return nil }
func (p *recordingCatalogProvider) SensitiveFields() []string { return nil }
func (p *recordingCatalogProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *recordingCatalogProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingCatalogProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogNode, error) {
	p.parent = parent
	return nil, nil
}
func (p *recordingCatalogProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return nil, nil
}

func TestScopeTableContentReaderUsesObjectCatalogReader(t *testing.T) {
	t.Parallel()

	reader, err := scopeTableContentReader(&PreviewRequest{
		Engine:   &models.Engine{EngineType: "minio", ID: 7},
		ItemType: "object",
		Schema:   "demo",
	}, nil, nil, plugin.ConnectionInfo{"bucket": "demo"})
	if err != nil {
		t.Fatalf("scopeTableContentReader() error = %v", err)
	}
	if _, ok := reader.(*objectCatalogContentReader); !ok {
		t.Fatalf("reader = %T, want *objectCatalogContentReader", reader)
	}
}

func TestScopeTableContentReaderUsesFileCatalogReader(t *testing.T) {
	t.Parallel()

	reader, err := scopeTableContentReader(&PreviewRequest{
		Engine:   &models.Engine{EngineType: "nfs", ID: 7},
		ItemType: "file",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("scopeTableContentReader() error = %v", err)
	}
	if _, ok := reader.(*fileCatalogContentReader); !ok {
		t.Fatalf("reader = %T, want *fileCatalogContentReader", reader)
	}
}

func TestObjectCatalogContentReaderListTrimsBucketFromScope(t *testing.T) {
	t.Parallel()

	catalog := &recordingCatalogProvider{}
	reader := newObjectCatalogContentReader(nil, catalog, nil, 7, "demo")
	if _, err := reader.List(context.Background(), contentio.NewRef("demo/dataset", contentio.RoleScope)); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got := catalog.parent.StringPath(); got != "demo/dataset" {
		t.Fatalf("catalog path = %q, want demo/dataset", got)
	}
}

func TestScopeTableInfoFromAttributes(t *testing.T) {
	t.Parallel()

	rowCount := int64(42)
	info, err := scopeTableInfoFromAttributes(map[string]interface{}{
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{
				"row_count": rowCount,
				"native":    map[string]interface{}{"partition_columns": []interface{}{"ds"}},
				"fields": []interface{}{
					map[string]interface{}{
						"name":        "id",
						"type":        "bigint",
						"native_type": "int64",
						"nullable":    false,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("scopeTableInfoFromAttributes() error = %v", err)
	}
	if info == nil || info.RowCount == nil || *info.RowCount != rowCount {
		t.Fatalf("table info row count = %#v, want %d", info, rowCount)
	}
	if len(info.Fields) != 1 || info.Fields[0].Name != "id" || info.Fields[0].Type != datatype.FieldTypeBigInt {
		t.Fatalf("fields = %#v", info.Fields)
	}
	if info.Native["partition_columns"] == nil {
		t.Fatalf("native = %#v, want partition_columns", info.Native)
	}
}

func TestScopeTableSampleOptionsFromAttributesUsesParquetFileRowCounts(t *testing.T) {
	t.Parallel()

	opts := scopeTableSampleOptionsFromAttributes(map[string]interface{}{
		"format": "parquet",
		"format_info": map[string]interface{}{
			"parquet": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"path": "/dataset/part-000.parquet", "row_count": 2},
					map[string]interface{}{"path": "dataset/part-001.parquet", "row_count": int64(3)},
				},
			},
		},
	})
	if opts == nil || opts.ExtraParams == nil {
		t.Fatal("expected parse options with parquet file row counts")
	}
	counts := map[string]int64{}
	for _, value := range opts.ExtraParams {
		if typed, ok := value.(map[string]int64); ok {
			counts = typed
			break
		}
	}
	if counts["dataset/part-000.parquet"] != 2 || counts["dataset/part-001.parquet"] != 3 {
		t.Fatalf("counts = %#v", counts)
	}
}
