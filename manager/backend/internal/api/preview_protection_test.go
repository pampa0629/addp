package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/gin-gonic/gin"
)

func TestPreviewJSONPreservesBigIntWithoutMutatingNativeRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		value interface{}
		want  interface{}
	}{
		{int64(9007199254740993), "9007199254740993"},
		{int64(-9007199254740993), "-9007199254740993"},
		{int64(9223372036854775807), "9223372036854775807"},
		{int64(-9223372036854775808), "-9223372036854775808"},
		{int64(1), "1"}, {int32(-1), "-1"},
		{json.Number("9007199254740993"), "9007199254740993"},
		{"9007199254740993", "9007199254740993"},
		{"***", "***"}, {nil, nil},
	} {
		row := map[string]interface{}{"id": tc.value, "small": 3, "amount": "0.000000000000000001"}
		table := &models.TablePreview{Mode: preview.PreviewModeTable, Columns: []string{"id", "small", "amount"},
			Fields:         []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "small", Type: datatype.FieldTypeInt}, {Name: "amount", Type: datatype.FieldTypeDecimal}},
			ColumnMetadata: []models.ColumnMetadata{{ColumnName: "id", Type: "bigint"}}, Rows: []map[string]interface{}{row}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		writePreviewResult(c, &preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}, nil)
		var response struct {
			Data struct {
				Rows           []map[string]interface{} `json:"rows"`
				ColumnMetadata []models.ColumnMetadata  `json:"column_metadata"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 {
			t.Fatalf("preview JSON failed: status=%d body=%s err=%v", w.Code, w.Body.String(), err)
		}
		if !reflect.DeepEqual(response.Data.Rows[0]["id"], tc.want) || response.Data.Rows[0]["small"] != float64(3) || response.Data.Rows[0]["amount"] != row["amount"] || response.Data.ColumnMetadata[0].Type != "bigint" {
			t.Fatalf("JSON cells lost types or precision: %#v", response.Data)
		}
		if !reflect.DeepEqual(row["id"], tc.value) || !reflect.DeepEqual(table.Rows[0]["id"], tc.value) {
			t.Fatal("JSON serialization changed the native bigint")
		}
	}
}

func TestPreviewBigIntJSONRejectsFloatAndInvalidProviderValues(t *testing.T) {
	for _, value := range []interface{}{float64(9007199254740993), float32(1), json.Number("1.0"), true, []byte("9007199254740993")} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		table := &models.TablePreview{Mode: preview.PreviewModeTable,
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
			Rows:   []map[string]interface{}{{"id": value}}}
		writePreviewResult(c, &preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}, nil)
		if w.Code != 500 || strings.Contains(w.Body.String(), "900719925474099") {
			t.Fatalf("invalid bigint was serialized: status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestPreviewBigIntJSONRunsAfterFieldProtection(t *testing.T) {
	seenNative := false
	table := &models.TablePreview{Mode: preview.PreviewModeTable, Columns: []string{"id"},
		Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
		Rows:   []map[string]interface{}{{"id": int64(9007199254740993)}},
		PreparedProtection: &dataprotection.PreparedTableProtection{Apply: func(result *plugin.QueryResult) error {
			seenNative = result.Rows[0]["id"] == int64(9007199254740993)
			result.Rows = []map[string]interface{}{{"id": "***"}}
			return nil
		}}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/preview", nil)
	writePreviewResult(c, &preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}, nil)
	if !seenNative || w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"***"`) || strings.Contains(w.Body.String(), "9007199254740993") {
		t.Fatalf("bigint protection order changed: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPreviewAuthorizationErrorsAreStableAndDoNotExposeUpstreamDetails(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{commonClient.ErrManagerPreviewCredentialRejected, 401},
		{commonClient.ErrManagerPreviewReadDenied, 403},
		{commonClient.ErrManagerPreviewReadUnavailable, 503},
		{preview.ErrPreviewMetadataUnavailable, 503},
		{preview.ErrSourceAuthorizationRequired, 403},
		{managerprotection.ErrRequired, 403},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		if !writePreviewAuthorizationError(c, errors.Join(test.err, errors.New("secret-current-credential"))) || w.Code != test.status || strings.Contains(w.Body.String(), "secret-current-credential") {
			t.Fatalf("unstable or unsafe authorization response: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestPreparedPreviewProtectionIsAppliedWithoutLeafRulesAndFiltersSpatialMetadata(t *testing.T) {
	for _, empty := range []bool{false, true} {
		calls := 0
		table := &models.TablePreview{
			Mode:    preview.PreviewModeTable,
			Columns: []string{"id", "shape"}, Rows: []map[string]interface{}{{"id": 1, "shape": "private"}},
			Fields:          []datatype.FieldInfo{{Name: "id"}, {Name: "shape"}},
			ColumnMetadata:  []models.ColumnMetadata{{ColumnName: "id"}, {ColumnName: "shape"}},
			GeometryColumns: []string{"shape"}, GeometryColumn: "shape", SRID: 4326, SourceSRID: 4326,
			SourceCRS: "EPSG:4326", TransformStatus: "not_transformed", PreviewHint: "spatial",
			Extent: []float64{1, 2, 3, 4},
			PreparedProtection: &dataprotection.PreparedTableProtection{Apply: func(result *plugin.QueryResult) error {
				calls++
				result.Columns = []string{"id"}
				if !empty {
					result.Rows = []map[string]interface{}{{"id": 1}}
				}
				return nil
			}},
		}
		if empty {
			table.Rows = nil
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		writePreviewResult(c, &preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}, nil)
		if w.Code != 200 || calls != 1 {
			t.Fatalf("prepared protection skipped or applied twice: status=%d calls=%d", w.Code, calls)
		}
		var response struct {
			Data struct {
				Rows    json.RawMessage `json:"rows"`
				Columns []string        `json:"columns"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !reflect.DeepEqual(response.Data.Columns, []string{"id"}) {
			t.Fatalf("unexpected protected response: %s err=%v", w.Body.String(), err)
		}
		if empty && string(response.Data.Rows) != "[]" {
			t.Fatalf("empty protected rows must serialize as an array: %s", response.Data.Rows)
		}
		if len(table.Columns) != 1 || len(table.Fields) != 1 || len(table.ColumnMetadata) != 1 || len(table.GeometryColumns) != 0 || table.GeometryColumn != "" || table.SRID != 0 || table.SourceSRID != 0 || table.SourceCRS != "" || table.Extent != nil || table.TransformStatus != "" || table.PreviewHint != "" {
			t.Fatalf("suppressed spatial metadata was retained: %#v", table)
		}
		if !empty {
			if _, exists := table.Rows[0]["shape"]; exists {
				t.Fatal("replacement protected rows not used")
			}
		}
	}
}

func TestPreparedPreviewProtectionFailureDoesNotFallbackToUnmanagedRows(t *testing.T) {
	for _, apply := range []func(*plugin.QueryResult) error{nil, func(*plugin.QueryResult) error { return errors.New("unsafe projection") }} {
		table := &models.TablePreview{PreparedProtection: &dataprotection.PreparedTableProtection{Apply: apply}}
		if !errors.Is(applyPreviewProtection(&preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}), managerprotection.ErrRequired) {
			t.Fatal("prepared protection failure was ignored")
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		writePreviewResult(c, &preview.PreviewResult{Data: table}, nil, dataprotection.SubjectReference{}, nil)
		if w.Code != 403 || strings.Contains(w.Body.String(), `"rows"`) {
			t.Fatalf("failed protection serialized preview data: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestPreviewResponseRowsArrayPreservesCellsAndNonTableMaterials(t *testing.T) {
	for _, test := range []struct {
		name string
		mode string
		rows []map[string]interface{}
		want string
	}{
		{name: "empty table", mode: preview.PreviewModeTable, want: "[]"},
		{name: "empty page", mode: preview.PreviewModeTable, rows: []map[string]interface{}{}, want: "[]"},
		{name: "null cell", mode: preview.PreviewModeTable, rows: []map[string]interface{}{{"phone": nil, "name": "retained"}}, want: `[{"name":"retained","phone":null}]`},
		{name: "non table", mode: "key_value", want: "null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			table := &models.TablePreview{Mode: test.mode, Columns: []string{"name", "phone"}, Rows: test.rows}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/preview", nil)
			writePreviewResult(c, &preview.PreviewResult{PreviewType: test.mode, Data: table}, nil, dataprotection.SubjectReference{}, nil)
			var response struct {
				Data struct {
					Rows json.RawMessage `json:"rows"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || string(response.Data.Rows) != test.want {
				t.Fatalf("unexpected preview response: %d %s err=%v", w.Code, w.Body.String(), err)
			}
		})
	}
}

func TestPreviewCatalogErrorDoesNotExposeNativeDetails(t *testing.T) {
	for kind, status := range map[plugin.EngineCatalogErrorKind]int{plugin.EngineCatalogErrorNotFound: 404, plugin.EngineCatalogErrorInvalidPath: 400, plugin.EngineCatalogErrorUnsupported: 400, plugin.EngineCatalogErrorUnavailable: 503} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		if !writeCatalogPreviewError(c, plugin.WrapEngineCatalogError(kind, errors.New("native secret-key-value"))) || w.Code != status || strings.Contains(w.Body.String(), "secret-key-value") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestPreviewProtectionRejectsNativeKeyWithoutFieldAdapter(t *testing.T) {
	for _, data := range []*models.TablePreview{
		{Mode: "key_value", KeyValue: &plugin.KeyValuePreview{}},
		{Mode: "key_value", Keyspace: &plugin.KeyValueDatasetPreview{}},
	} {
		result := &preview.PreviewResult{PreviewType: "key_value", Data: data}
		if err := applyPreviewProtection(result, []dataprotection.Rule{{}}, dataprotection.SubjectReference{}); err != managerprotection.ErrRequired {
			t.Fatalf("native protection bypass: %v", err)
		}
		if err := applyPreviewProtection(result, nil, dataprotection.SubjectReference{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreviewProtectionSuppressesOutputColumnAndMetadata(t *testing.T) {
	subject := dataprotection.SubjectReference{Type: "user", ID: "41"}
	now := time.Now().UTC()
	for _, test := range []struct {
		name       string
		empty      bool
		authorized bool
	}{
		{name: "rows"},
		{name: "empty page", empty: true},
		{name: "authorized original", authorized: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rule := dataprotection.Rule{
				Action:    managerprotection.ActionPreview,
				Component: dataprotection.Component{Key: "email", Path: []dataprotection.PathSegment{{Name: "email", Container: "scalar"}}, ValueType: "string"},
				Decision:  dataprotection.Decision{Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress},
			}
			if test.authorized {
				rule.Authorizations = []dataprotection.TemporaryAuthorization{{Subject: subject, Effect: dataprotection.EffectAllow, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute)}}
			}
			table := &models.TablePreview{
				Columns:        []string{"id", "email", "customer_code"},
				ColumnMetadata: []models.ColumnMetadata{{ColumnName: "id"}, {ColumnName: "email"}, {ColumnName: "customer_code"}},
				Rows:           []map[string]interface{}{{"id": 1, "email": "private@example.test", "customer_code": "customer-1"}},
			}
			if test.empty {
				table.Rows = nil
			}
			result := &preview.PreviewResult{PreviewType: "table", Data: table}
			if err := applyPreviewProtection(result, []dataprotection.Rule{rule}, subject); err != nil {
				t.Fatal(err)
			}
			want := []string{"id", "customer_code"}
			if test.authorized {
				want = []string{"id", "email", "customer_code"}
			}
			if !reflect.DeepEqual(table.Columns, want) {
				t.Fatalf("output columns = %v, want %v", table.Columns, want)
			}
			metadata := make([]string, 0, len(table.ColumnMetadata))
			for _, column := range table.ColumnMetadata {
				metadata = append(metadata, column.ColumnName)
			}
			if !reflect.DeepEqual(metadata, want) {
				t.Fatalf("column metadata = %v, want %v", metadata, want)
			}
			if !test.empty {
				row := table.Rows[0]
				if _, present := row["email"]; present != test.authorized || row["id"] != 1 || row["customer_code"] != "customer-1" {
					t.Fatal("protected preview changed its value contract")
				}
			}
		})
	}
}

func TestPreviewProtectionMasksOutdoorPhoneAtResponseBoundary(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	req := outdoorPersonsPreviewRequest()
	projection := activeOutdoorPhoneProjection(t, req, now)

	rules, err := managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive,
		Projections: []dataprotection.Projection{projection},
	}, managerprotection.ActionPreview, now)
	if err != nil {
		t.Fatalf("previewProtectionRules() error = %v", err)
	}
	result := &preview.PreviewResult{
		PreviewType: "table",
		Data: &models.TablePreview{Rows: []map[string]interface{}{
			{"_id": "person-1", "userInfo": map[string]interface{}{"phone": "13661384499", "nickName": "daydayup"}},
		}},
	}
	if err := applyPreviewProtection(result, rules, dataprotection.SubjectReference{}); err != nil {
		t.Fatalf("applyPreviewProtection() error = %v", err)
	}
	row := result.Data.(*models.TablePreview).Rows[0]
	userInfo := row["userInfo"].(map[string]interface{})
	if got := userInfo["phone"]; got != "136****4499" {
		t.Fatalf("protected phone = %#v, want 136****4499", got)
	}
	if got := userInfo["nickName"]; got != "daydayup" {
		t.Fatalf("unmanaged field changed: %#v", got)
	}
}

func TestPreviewResponseRejectsProjectionExpiredAfterAdmission(t *testing.T) {
	for _, empty := range []bool{false, true} {
		now := time.Now().UTC()
		req := outdoorPersonsPreviewRequest()
		projection := activeOutdoorPhoneProjection(t, req, now.Add(-2*time.Hour))
		rules, err := managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), projectionstore.GateResult{
			Managed: true, State: dataprotection.ProjectionStateActive, Projections: []dataprotection.Projection{projection},
		}, managerprotection.ActionPreview, projection.ExpiresAt.Add(-time.Nanosecond))
		if err != nil {
			t.Fatal(err)
		}
		table := &models.TablePreview{Columns: []string{"userInfo"}, Rows: []map[string]interface{}{{"userInfo": map[string]interface{}{"phone": "13661384499"}}}}
		if empty {
			table.Rows = nil
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		writePreviewResult(c, &preview.PreviewResult{Data: table}, rules, dataprotection.SubjectReference{}, []dataprotection.Projection{projection})
		if w.Code != 403 || strings.Contains(w.Body.String(), "13661384499") || strings.Contains(w.Body.String(), `"rows"`) || strings.Contains(w.Body.String(), `"columns"`) {
			t.Fatalf("expired preview serialized data: status=%d", w.Code)
		}
		if !empty && table.Rows[0]["userInfo"].(map[string]interface{})["phone"] != "13661384499" {
			t.Fatal("expiry must reject before transforming preview rows")
		}
	}
}

func TestPreviewProtectionSuppressesTooShortStructuredValueWithoutLeakingIt(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	req := outdoorPersonsPreviewRequest()
	projection := activeOutdoorPhoneProjection(t, req, now)
	rules, err := managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive,
		Projections: []dataprotection.Projection{projection},
	}, managerprotection.ActionPreview, now)
	if err != nil {
		t.Fatal(err)
	}
	result := &preview.PreviewResult{
		PreviewType: "table",
		Data: &models.TablePreview{Rows: []map[string]interface{}{
			{"userInfo": map[string]interface{}{"phone": "123"}},
		}},
	}
	if err := applyPreviewProtection(result, rules, dataprotection.SubjectReference{}); err != nil {
		t.Fatalf("applyPreviewProtection() error = %v", err)
	}
	userInfo := result.Data.(*models.TablePreview).Rows[0]["userInfo"].(map[string]interface{})
	if _, exists := userInfo["phone"]; exists {
		t.Fatalf("invalid protected structured value was returned: %#v", userInfo)
	}
}

func TestPreviewProtectionFailsClosedForEnrollingOrSchemaDrift(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	req := outdoorPersonsPreviewRequest()
	if _, err := managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateEnrolling,
	}, managerprotection.ActionPreview, now); err == nil {
		t.Fatal("enrolling projection did not fail closed")
	}

	projection := activeOutdoorPhoneProjection(t, req, now)
	drifted := outdoorPersonsPreviewRequest()
	table := datatype.TableInfoFromPayload(drifted.Metadata.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{}), drifted.ItemName)
	table.Fields[2].Type = datatype.FieldTypeBigInt
	drifted.Metadata.Attributes["type_info"] = map[string]interface{}{"table": datatype.TableInfoPayload(table)}
	if _, err := managerprotection.TableRules(drifted.ItemFingerprint, drifted.TableFields(), projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive,
		Projections: []dataprotection.Projection{projection},
	}, managerprotection.ActionPreview, now); err == nil {
		t.Fatal("schema drift did not fail closed")
	}
}

func TestPreviewProtectionLeavesUnmanagedResourceOnOriginalPath(t *testing.T) {
	req := outdoorPersonsPreviewRequest()
	rules, err := managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), projectionstore.GateResult{}, managerprotection.ActionPreview, time.Now().UTC())
	if err != nil || len(rules) != 0 {
		t.Fatalf("unmanaged rules = %#v, error = %v", rules, err)
	}
	result := &preview.PreviewResult{Data: &models.TablePreview{Rows: []map[string]interface{}{
		{"userInfo": map[string]interface{}{"phone": "13661384499"}},
	}}}
	if err := applyPreviewProtection(result, rules, dataprotection.SubjectReference{}); err != nil {
		t.Fatalf("unmanaged response changed path: %v", err)
	}
	if got := result.Data.(*models.TablePreview).Rows[0]["userInfo"].(map[string]interface{})["phone"]; got != "13661384499" {
		t.Fatalf("unmanaged phone changed: %#v", got)
	}
}

func outdoorPersonsPreviewRequest() *preview.PreviewResolverRequest {
	fields := []datatype.FieldInfo{
		{Name: "_id", Path: []string{"_id"}, Type: datatype.FieldTypeString},
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	return &preview.PreviewResolverRequest{
		ItemName:        "Persons",
		ItemFingerprint: "sha256:outdoor-persons",
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": datatype.TableInfoPayload(&datatype.TableInfo{Name: "Persons", Fields: fields}),
			},
		}},
	}
}

func activeOutdoorPhoneProjection(t *testing.T, req *preview.PreviewResolverRequest, now time.Time) dataprotection.Projection {
	t.Helper()
	fields := req.TableFields()
	component := dataprotection.Component{
		Key: "userInfo.phone", ValueType: string(datatype.FieldTypeString),
		Path: []dataprotection.PathSegment{
			{Name: "userInfo", Container: "object"},
			{Name: "phone", Container: "scalar"},
		},
	}
	fingerprint, err := dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	component.SchemaFingerprint = fingerprint
	snapshotHash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV3,
		ProjectionID:  "outdoor-phone-manager",
		Revision:      "00000000000000000001",
		ConsumerOwner: "manager",
		State:         dataprotection.ProjectionStateActive,
		Target: dataprotection.ResourceReference{
			OwnerModule: "meta", ResourceType: "data_item",
			ResourceIdentity: req.ItemFingerprint, ComponentKey: "userInfo.phone",
		},
		SourceSnapshotHash: snapshotHash,
		Rules: []dataprotection.Rule{
			{
				Action:    managerprotection.ActionPreview,
				Component: component,
				Decision: dataprotection.Decision{
					Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2,
					Parameters: map[string]any{
						"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*",
					},
					InvalidValueEffect: dataprotection.EffectSuppress,
				},
			},
			{
				Action: managerprotection.ActionProfile, Component: component,
				Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress},
			},
		},
		ValidFrom: now.Add(-time.Hour), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	return projection
}
