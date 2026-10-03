package preview

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	es "github.com/addp/common/engine/plugins/elasticsearch"
	"github.com/addp/manager/internal/models"
	"os"
	"testing"
)

type documentPreviewStub struct {
	es.ElasticsearchPlugin
	rows             []map[string]interface{}
	position, closed int
	fail             bool
}

func (p *documentPreviewStub) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "items.sku", Path: []string{"items", "sku"}, Type: datatype.FieldTypeString}}}}, nil
}
func (p *documentPreviewStub) OpenRecordReadSession(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.RecordReadSessionOptions) (plugin.RecordReadSession, error) {
	return p, nil
}
func (p *documentPreviewStub) ReadBatch(_ context.Context, n int) (*plugin.RecordBatchData, error) {
	if p.fail {
		return nil, errors.New("failed")
	}
	end := min(p.position+n, len(p.rows))
	batch := &plugin.RecordBatchData{Records: p.rows[p.position:end]}
	p.position = end
	return batch, nil
}
func (p *documentPreviewStub) Close(context.Context) error { p.closed++; return nil }
func TestDocumentRecordSetPreviewPaginationAndCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		p := &documentPreviewStub{fail: fail, rows: []map[string]interface{}{{"id": "first"}, {"id": json.Number("9007199254740993"), "items": []interface{}{map[string]interface{}{"sku": "a"}}, "optional": nil}}}
		count := int64(2383)
		result, err := NewDocumentRecordSetPreviewProvider().Preview(context.Background(), &PreviewRequest{Engine: &models.Engine{ID: 11, EngineType: "elasticsearch"}, EnginePlugin: p, Page: 2, PageSize: 1, ItemRowCount: &count})
		if p.closed != 1 {
			t.Fatal("session leak")
		}
		if fail {
			if err == nil {
				t.Fatal("failure accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != 2383 || len(result.Rows) != 1 || result.Rows[0]["id"].(json.Number).String() != "9007199254740993" {
			t.Fatal(result)
		}
		if _, exists := result.Rows[0]["missing"]; exists {
			t.Fatal("missing replaced")
		}
		if len(result.ColumnMetadata[0].Path) != 2 {
			t.Fatal(result.ColumnMetadata)
		}
	}
}
func TestIntegrationElasticsearchPreview(t *testing.T) {
	if os.Getenv("ADDP_ELASTICSEARCH_INTEGRATION") != "1" {
		t.Skip("owned T2 required")
	}
	p := &es.ElasticsearchPlugin{}
	path := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 91)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "index", Kind: "index", Name: "addp_orders.v1"})
	result, err := NewDocumentRecordSetPreviewProvider().Preview(context.Background(), &PreviewRequest{Engine: &models.Engine{ID: 91, EngineType: p.Type(), ConnectionInfo: models.ConnectionInfo{"endpoint": os.Getenv("ELASTICSEARCH_ENDPOINT"), "user": os.Getenv("ELASTICSEARCH_READER_USER"), "password": os.Getenv("ELASTICSEARCH_READER_PASSWORD")}}, EnginePlugin: p, ProviderPath: path, Table: "addp_orders.v1", Page: 2, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 25 || len(result.Rows) != 5 || len(result.Fields) == 0 || result.Table != "addp_orders.v1" {
		t.Fatal(result)
	}
	if _, ok := result.Rows[0]["customer"].(map[string]interface{}); !ok {
		t.Fatal("nested source lost")
	}
}
