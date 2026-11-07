package preview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	es "github.com/addp/common/engine/plugins/elasticsearch"
	"github.com/addp/manager/internal/models"
)

type documentPreviewStub struct {
	es.ElasticsearchPlugin
	rows             []map[string]interface{}
	position, closed int
	fail             bool
	describeOptions  plugin.EngineCatalogFactsOptions
	readLimits       []int
}

func (p *documentPreviewStub) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.describeOptions = opts
	estimate := int64(9000)
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{
		Fields:            []datatype.FieldInfo{{Name: "items.sku", Path: []string{"items", "sku"}, Type: datatype.FieldTypeString}},
		EstimatedRowCount: &estimate,
	}}, nil
}
func (p *documentPreviewStub) OpenRecordReadSession(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.RecordReadSessionOptions) (plugin.RecordReadSession, error) {
	return p, nil
}
func (p *documentPreviewStub) ReadBatch(_ context.Context, n int) (*plugin.RecordBatchData, error) {
	p.readLimits = append(p.readLimits, n)
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
		if (result.Total == nil || *result.Total != 2383) || len(result.Rows) != 1 || result.Rows[0]["id"].(json.Number).String() != "9007199254740993" {
			t.Fatal(result)
		}
		if _, exists := result.Rows[0]["missing"]; exists {
			t.Fatal("missing replaced")
		}
		if len(result.ColumnMetadata[0].Path) != 2 {
			t.Fatal(result.ColumnMetadata)
		}
		if result.HasMore || !slices.Equal(p.readLimits, []int{1, 1}) {
			t.Fatalf("known total should only read the requested page: more=%v limits=%v", result.HasMore, p.readLimits)
		}
	}
}

func TestDocumentRecordSetPreviewUsesLookaheadWithoutStatistics(t *testing.T) {
	for _, tc := range []struct {
		name           string
		page           int
		count          int
		wantRows       int
		wantMore       bool
		wantReadLimits []int
	}{
		{name: "next page", page: 1, count: 2, wantRows: 1, wantMore: true, wantReadLimits: []int{2}},
		{name: "full last page", page: 2, count: 2, wantRows: 1, wantReadLimits: []int{1, 2, 1}},
		{name: "empty page", page: 2, count: 1, wantReadLimits: []int{1, 2}},
		{name: "preview boundary", page: 1000, count: 1001, wantRows: 1, wantReadLimits: []int{999, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]map[string]interface{}, tc.count)
			for i := range rows {
				rows[i] = map[string]interface{}{"id": i + 1}
			}
			if tc.wantMore {
				rows[tc.page]["probe_only"] = true
			}
			p := &documentPreviewStub{rows: rows}
			result, err := NewDocumentRecordSetPreviewProvider().Preview(t.Context(), &PreviewRequest{
				Engine: &models.Engine{ID: 11, EngineType: "elasticsearch"}, EnginePlugin: p, Page: tc.page, PageSize: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if p.describeOptions.IncludeStatistics {
				t.Fatal("preview requested live statistics")
			}
			if result.Total != nil || result.HasMore != tc.wantMore || len(result.Rows) != tc.wantRows {
				t.Fatalf("pagination: total=%v more=%v rows=%d", result.Total, result.HasMore, len(result.Rows))
			}
			if slices.Contains(result.Columns, "probe_only") || !slices.Equal(p.readLimits, tc.wantReadLimits) || p.closed != 1 {
				t.Fatalf("page columns=%v read limits=%v session closes=%d", result.Columns, p.readLimits, p.closed)
			}
		})
	}
}

func TestIntegrationElasticsearchPreview(t *testing.T) {
	if os.Getenv("ADDP_ELASTICSEARCH_INTEGRATION") != "1" {
		t.Skip("owned T2 required")
	}
	p := &es.ElasticsearchPlugin{}
	path := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 91)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "index", Kind: "index", Name: "addp_orders.v1"})
	provider := NewDocumentRecordSetPreviewProvider()
	req := &PreviewRequest{
		Engine: &models.Engine{ID: 91, EngineType: p.Type(), ConnectionInfo: models.ConnectionInfo{
			"endpoint": os.Getenv("ELASTICSEARCH_ENDPOINT"), "user": os.Getenv("ELASTICSEARCH_READER_USER"), "password": os.Getenv("ELASTICSEARCH_READER_PASSWORD"),
		}},
		EnginePlugin: p, ProviderPath: path, Table: "addp_orders.v1", PageSize: 20,
	}
	for i, wantRows := range []int{20, 5} {
		req.Page = i + 1
		result, err := provider.Preview(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != nil || result.HasMore != (i == 0) || len(result.Rows) != wantRows || len(result.Fields) == 0 || result.Table != req.Table {
			t.Fatalf("unexpected preview page %d: %#v", req.Page, result)
		}
		if _, ok := result.Rows[0]["customer"].(map[string]interface{}); !ok {
			t.Fatal("nested source lost")
		}
	}
}
