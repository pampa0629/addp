package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/gin-gonic/gin"
)

// Exercise the HTTP owner, resolver and native database provider together. Meta
// deliberately stays unchanged while the live catalog gains an unrelated field.
func TestPreviewRejectsLiveSchemaDriftBeforeReadingAndRecovers(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString, Nullable: true}}
	provider := &liveSchemaPreviewPlugin{
		apiDownloadTestFilePlugin: newAPIDownloadTestFilePlugin("preview-live-schema", nil),
		fields:                    fields,
	}
	plugin.Register(provider)
	t.Cleanup(func() { plugin.Unregister(provider.Type()) })
	attributes := map[string]interface{}{
		"item":      map[string]interface{}{"data_type": "table", "layout": "single"},
		"type_info": map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{Name: "cards", Fields: fields})},
	}
	itemJSON, err := json.Marshal(map[string]interface{}{
		"id": 1, "tenant_id": 1, "engine_id": 9, "item_type": "table", "name": "cards", "full_name": "business.cards",
		"scanned_depth": "deep", "attributes": attributes,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := preview.NewPreviewRegistry()
	registry.Register(preview.NewDatabaseTablePreviewProvider(nil, nil))
	resolver := preview.NewPreviewResolver(registry, apiDownloadTestSystemClient(t, 9, provider.Type()), apiDownloadTestMetaItemClient(t, string(itemJSON)))
	now := time.Now().UTC()
	component := dataprotection.Component{Key: "phone", ValueType: "string", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}}
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV5, TargetEngineID: 9,
		ProjectionID: "preview-live-schema", Revision: "00000000000000000001", ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: commonModels.GenerateItemFingerprint(9, "business.cards"), ComponentKey: "phone"},
		SourceSnapshotHash: snapshot, ValidFrom: now.Add(-time.Hour), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
		Rules: []dataprotection.Rule{{Action: managerprotection.ActionPreview, Component: component, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2,
			Parameters: map[string]any{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, InvalidValueEffect: dataprotection.EffectSuppress,
		}}},
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	gate := &liveSchemaPreviewGate{result: projectionstore.GateResult{Managed: true, State: dataprotection.ProjectionStateActive, Projections: []dataprotection.Projection{projection}}}
	handler := NewExplorerHandler(nil, resolver, nil, gate, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 33); c.Next() })
	router.GET("/preview", handler.Preview)
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/preview?locator="+urlQueryEscape("addp://engine/9/path/business/cards?type=table&item_id=1"), nil))
		return w
	}
	assertPhone := func(w *httptest.ResponseRecorder, want string) {
		t.Helper()
		var body struct {
			Data struct {
				Rows []map[string]interface{} `json:"rows"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || len(body.Data.Rows) != 1 || body.Data.Rows[0]["phone"] != want {
			t.Fatalf("unexpected preview: status=%d rows=%d decode=%v", w.Code, len(body.Data.Rows), err)
		}
	}
	assertPhone(request(), "138****0000")
	if provider.reads != 1 {
		t.Fatalf("baseline reads=%d", provider.reads)
	}
	for _, drift := range []struct {
		name   string
		fields []datatype.FieldInfo
	}{
		{"added unrelated field", append(append([]datatype.FieldInfo(nil), fields...), datatype.FieldInfo{Name: "new_column", Type: datatype.FieldTypeInt, Nullable: true})},
		{"changed type", []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeBigInt, Nullable: true}}},
		{"changed nullability", []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}}},
		{"missing structure", nil},
	} {
		t.Run(drift.name, func(t *testing.T) {
			provider.fields = drift.fields
			denied := request()
			var failure map[string]interface{}
			if err := json.Unmarshal(denied.Body.Bytes(), &failure); err != nil || denied.Code != 403 || failure["error_code"] != "security_protection_required" || len(failure) != 2 {
				t.Fatalf("live structure drift was not safely rejected: status=%d decode=%v", denied.Code, err)
			}
			if provider.reads != 1 {
				t.Fatal("schema mismatch must reject before reading business rows")
			}
		})
	}
	provider.fields = fields
	assertPhone(request(), "138****0000")
	// A matching schema check must preserve the original subject-aware response
	// protection, including an explicitly approved short-lived original value.
	gate.result.Projections[0].Rules[0].Authorizations = []dataprotection.TemporaryAuthorization{{
		Subject: dataprotection.SubjectReference{Type: "user", ID: "33"}, Effect: dataprotection.EffectAllow,
		ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute),
	}}
	if err := gate.result.Projections[0].Seal(); err != nil {
		t.Fatal(err)
	}
	assertPhone(request(), "13800000000")
	if provider.reads != 3 {
		t.Fatalf("recovered/authorized reads=%d", provider.reads)
	}
}

type liveSchemaPreviewGate struct{ result projectionstore.GateResult }

func (g *liveSchemaPreviewGate) Gate(int64, dataprotection.ResourceReference, time.Time) projectionstore.GateResult {
	return g.result
}
func (*liveSchemaPreviewGate) RequireUnmanaged(context.Context, int64, []dataprotection.ResourceReference, time.Time) error {
	return managerprotection.ErrRequired
}

type liveSchemaPreviewPlugin struct {
	*apiDownloadTestFilePlugin
	fields []datatype.FieldInfo
	reads  int
}

func (p *liveSchemaPreviewPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), plugin.EngineCatalogTermDatabase, plugin.TabularCapabilityOptions{})
}
func (*liveSchemaPreviewPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase)
}
func (*liveSchemaPreviewPlugin) SQLDialect() string { return "mysql" }
func (p *liveSchemaPreviewPlugin) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Fields: append([]datatype.FieldInfo(nil), p.fields...)}}, nil
}
func (p *liveSchemaPreviewPlugin) ReadBatch(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.BatchReadOptions) (*plugin.BatchData, error) {
	p.reads++
	return &plugin.BatchData{Rows: []map[string]interface{}{{"phone": "13800000000"}}}, nil
}
