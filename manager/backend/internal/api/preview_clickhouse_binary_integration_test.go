package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/clickhouse"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestIntegrationManagerClickHouseBinaryPreview(t *testing.T) {
	if os.Getenv("ADDP_CLICKHOUSE_INTEGRATION") != "1" {
		t.Skip("requires the disposable ClickHouse gate")
	}
	port, err := strconv.Atoi(os.Getenv("ADDP_TEST_CLICKHOUSE_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	dbName := os.Getenv("ADDP_TEST_CLICKHOUSE_DATABASE")
	if dbName != "addp_clickhouse_disposable" || os.Getenv("ADDP_TEST_CLICKHOUSE_HOST") != "127.0.0.1" {
		t.Fatal("expected the disposable ClickHouse database")
	}
	ctx := t.Context()
	const engineID = 9
	tableName := "manager_bytes_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	info := plugin.ConnectionInfo{
		"host": "127.0.0.1", "port": port, "database": dbName,
		"user": os.Getenv("ADDP_TEST_CLICKHOUSE_USER"), "password": os.Getenv("ADDP_TEST_CLICKHOUSE_PASSWORD"),
	}
	provider := &clickhouse.ClickHousePlugin{}
	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, dbName, tableName)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := provider.DeleteResource(ctx, info, path); err != nil {
			t.Error(err)
		}
	})
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeInt},
		{Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "label", Type: datatype.FieldTypeString},
	}
	if err := provider.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	raw := []byte{0x41, 0, 0xff, 0x42}
	rows := []map[string]interface{}{
		{"id": 1, "payload": raw, "label": "中文"},
		{"id": 2, "payload": []byte("AB"), "label": "AB"},
		{"id": 3, "payload": []byte{}, "label": ""},
		{"id": 4, "payload": nil, "label": "null"},
	}
	if err := provider.WriteBatch(ctx, info, path, &plugin.BatchData{Fields: fields, Rows: rows}, plugin.BatchWriteOptions{}); err != nil {
		t.Fatal(err)
	}
	facts, err := provider.DescribeEngineCatalogFacts(ctx, info, path, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := provider.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	qualified := dbName + "." + tableName
	var nativeHex string
	if err := db.QueryRowContext(ctx, "SELECT hex(payload) FROM "+qualified+" WHERE id=1").Scan(&nativeHex); err != nil || nativeHex != "4100FF42" {
		t.Fatalf("ClickHouse stored hex=%q error=%v", nativeHex, err)
	}

	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/engines/9" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": engineID, "tenant_id": 1, "name": "disposable", "engine_type": "clickhouse",
			"connection_info": info, "version": 1, "lifecycle_state": "active", "connection_status": "online",
		})
	}))
	defer system.Close()
	token := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "test-token", nil })
	systemClient := client.NewSystemClient(system.URL, token)
	fullName := dbName + "." + tableName
	request := func(t *testing.T, metaFields []datatype.FieldInfo, gate *liveSchemaPreviewGate) *httptest.ResponseRecorder {
		t.Helper()
		metadata, err := json.Marshal(map[string]interface{}{
			"id": 1, "tenant_id": 1, "engine_id": engineID, "item_type": "table",
			"name": tableName, "full_name": fullName, "scanned_depth": "deep",
			"attributes": map[string]interface{}{
				"item":      map[string]interface{}{"data_type": "table", "layout": "single"},
				"type_info": map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{Name: tableName, Fields: metaFields})},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		metaClient := apiDownloadTestMetaItemClient(t, string(metadata))
		registry := preview.NewPreviewRegistry()
		registry.Register(preview.NewDatabaseTablePreviewProvider(nil, metaClient))
		resolver := preview.NewPreviewResolver(registry, systemClient, metaClient)
		handler := NewExplorerHandler(nil, resolver, nil, gate, nil)
		router := gin.New()
		router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 1, 33); c.Next() })
		router.GET("/api/v1/manager/preview", handler.Preview)
		locator := "addp://engine/9/path/" + dbName + "/" + tableName + "?type=table&item_id=1"
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/manager/preview?page=1&page_size=4&locator="+url.QueryEscape(locator), nil))
		return res
	}
	type previewData struct {
		ColumnMetadata []struct {
			Name string `json:"column_name"`
			Type string `json:"type"`
		} `json:"column_metadata"`
		Rows []map[string]interface{} `json:"rows"`
	}
	read := func(t *testing.T, res *httptest.ResponseRecorder) previewData {
		t.Helper()
		var body struct {
			Data previewData `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != 200 || len(body.Data.Rows) != 4 {
			t.Fatalf("preview status=%d body=%s err=%v", res.Code, res.Body.String(), err)
		}
		return body.Data
	}
	staleFields := datatype.CloneFieldInfos(facts.Table.Fields)
	staleFields[1].Type = datatype.FieldTypeString
	unmanaged := &liveSchemaPreviewGate{}
	data := read(t, request(t, staleFields, unmanaged))
	if len(data.ColumnMetadata) != 3 || data.ColumnMetadata[1].Name != "payload" || data.ColumnMetadata[1].Type != "bytes" {
		t.Fatalf("preview ignored live type: %#v", data.ColumnMetadata)
	}
	byID := make(map[float64]map[string]interface{}, len(data.Rows))
	for _, row := range data.Rows {
		byID[row["id"].(float64)] = row
	}
	decoded, err := base64.StdEncoding.DecodeString(byID[1]["payload"].(string))
	if err != nil || !bytes.Equal(decoded, raw) || byID[1]["label"] != "中文" ||
		byID[2]["payload"] != "QUI=" || byID[2]["label"] != "AB" ||
		byID[3]["payload"] != "" || byID[4]["payload"] != nil {
		t.Fatalf("unexpected preview rows: %#v, decode=%v", byID, err)
	}
	t.Logf("ClickHouse hex=%s Manager Base64=%s decoded hex=%X", nativeHex, byID[1]["payload"], decoded)

	now := time.Now().UTC()
	snapshot, err := dataprotection.TableSchemaSnapshotHash(facts.Table.Fields)
	if err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV5, TargetEngineID: engineID,
		ProjectionID: "clickhouse-preview-binary", Revision: "00000000000000000001", ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: commonModels.GenerateItemFingerprint(engineID, fullName)},
		SourceSnapshotHash: snapshot, ValidFrom: now.Add(-time.Minute), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
	}
	for _, tc := range []struct {
		name, kind string
		decision   dataprotection.Decision
	}{
		{"payload", "bytes", dataprotection.Decision{Effect: dataprotection.EffectSuppress}},
		{"label", "string", dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmConstantV1, Parameters: map[string]interface{}{"value": "***"}}},
	} {
		component := dataprotection.Component{Key: tc.name, ValueType: tc.kind, Path: []dataprotection.PathSegment{{Name: tc.name, Container: "scalar"}}}
		component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(facts.Table.Fields, component)
		if err != nil {
			t.Fatal(err)
		}
		projection.Rules = append(projection.Rules, dataprotection.Rule{Action: "preview", Component: component, Decision: tc.decision})
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	managed := &liveSchemaPreviewGate{result: projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive, Projections: []dataprotection.Projection{projection},
	}}
	protected := request(t, facts.Table.Fields, managed)
	protectedData := read(t, protected)
	if len(protectedData.ColumnMetadata) != 2 || strings.Contains(protected.Body.String(), base64.StdEncoding.EncodeToString(raw)) {
		t.Fatalf("protected field leaked: %s", protected.Body.String())
	}
	for _, row := range protectedData.Rows {
		if _, exists := row["payload"]; exists || row["label"] != "***" {
			t.Fatalf("unexpected protected row: %#v", row)
		}
	}

	if _, err := db.ExecContext(ctx, "ALTER TABLE "+qualified+" COMMENT COLUMN payload ''"); err != nil {
		t.Fatal(err)
	}
	denied := request(t, facts.Table.Fields, managed)
	if denied.Code != 403 || !strings.Contains(denied.Body.String(), "security_protection_required") {
		t.Fatalf("managed schema drift was not rejected: %d %s", denied.Code, denied.Body.String())
	}
	invalid := request(t, facts.Table.Fields, unmanaged)
	if invalid.Code != 422 || !strings.Contains(invalid.Body.String(), "preview_text_encoding_invalid") ||
		strings.Contains(invalid.Body.String(), "rows") || strings.Contains(invalid.Body.String(), "\\ufffd") {
		t.Fatalf("unmarked invalid text was not diagnosed safely: %d %s", invalid.Code, invalid.Body.String())
	}
}
