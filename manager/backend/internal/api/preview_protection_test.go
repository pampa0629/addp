package api

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
)

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
		SchemaVersion: dataprotection.ProjectionSchemaV2,
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
		ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	return projection
}
