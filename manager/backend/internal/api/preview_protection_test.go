package api

import (
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
)

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
	if err := applyPreviewProtection(result, rules); err != nil {
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

func TestPreviewProtectionSuppressesInvalidPhoneWithoutLeakingValue(t *testing.T) {
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
			{"userInfo": map[string]interface{}{"phone": "136ABCD4499"}},
		}},
	}
	if err := applyPreviewProtection(result, rules); err != nil {
		t.Fatalf("applyPreviewProtection() error = %v", err)
	}
	userInfo := result.Data.(*models.TablePreview).Rows[0]["userInfo"].(map[string]interface{})
	if _, exists := userInfo["phone"]; exists {
		t.Fatalf("invalid protected phone was returned: %#v", userInfo)
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
	if err := applyPreviewProtection(result, rules); err != nil {
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
		SchemaVersion: dataprotection.ProjectionSchemaV1,
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
					Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
					Parameters: map[string]any{
						"prefix_runes": 3, "suffix_runes": 4, "replacement": "****",
						"exact_runes": 11, "character_class": "ascii_digit",
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
