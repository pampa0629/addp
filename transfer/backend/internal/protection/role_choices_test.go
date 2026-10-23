package protection

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
)

func installTransferRoleOption(t *testing.T, store *projectionstore.Store, model plugin.EngineCatalogModelSpec, path plugin.EngineCatalogPath, fields []datatype.FieldInfo) dataprotection.FieldRoleChoice {
	t.Helper()
	installActiveTransferProjection(t, store, model, path, fields)
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		t.Fatal(err)
	}
	projection := store.Gate(7, target, time.Now()).Projections[0]
	projection.Revision = "00000000000000000002"
	options := &dataprotection.RoleMaskingOptions{PolicyID: uuid.NewString(), AssessmentRevision: 1, PolicyRevision: 1, Options: []dataprotection.RoleMaskingOption{{ID: uuid.NewString(), Name: "统计哈希", RoleKeys: []string{"custom.analytics"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]any{}, InvalidValueEffect: dataprotection.EffectSuppress}}}}
	projection.Rules[0].RoleOptions = options
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "cursor-transfer-active", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "role-choice", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "role-choice"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	return dataprotection.FieldRoleChoice{Target: target, ComponentKey: projection.Rules[0].Component.Key, Selection: dataprotection.RoleOptionSelection{PolicyID: options.PolicyID, PolicyRevision: 1, AssessmentRevision: 1, OptionID: options.Options[0].ID}}
}
func TestBoundedRoleProtectionRefreshesEachBatchAndRejectsLostQualification(t *testing.T) {
	store := transferProjectionStore(t)
	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
	path := plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "persons")
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	choice := installTransferRoleOption(t, store, model, path, fields)
	gate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "postgresql"}}.descriptor)
	config := map[string]any{"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"}, "source": map[string]any{"locator": "addp://engine/11/path/public/persons?type=table", "data_type": "table", "representation": "native", "protection_choices": []dataprotection.FieldRoleChoice{choice}}}
	roles := []string{"custom.analytics"}
	reads := 0
	resolve := func(context.Context) (*dataprotection.QueryRoleSelection, error) {
		reads++
		return &dataprotection.QueryRoleSelection{Choices: []dataprotection.FieldRoleChoice{choice}, RoleKeys: roles, ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	if _, err := gate.PrepareBoundedTableProtection(t.Context(), 7, config, nil); err == nil {
		t.Fatal("missing execution proof accepted")
	}
	bound, err := gate.PrepareBoundedTableProtection(t.Context(), 7, config, resolve)
	if err != nil {
		t.Fatal(err)
	}
	protect, err := bound.PrepareCatalogTableProtection(t.Context(), path, fields)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result := &plugin.QueryResult{Columns: []string{"phone"}, Rows: []map[string]any{{"phone": "13661384499"}}}
		if err := protect.Apply(result); err != nil {
			t.Fatal(err)
		}
		if value, ok := result.Rows[0]["phone"].(string); !ok || len(value) != 64 {
			t.Fatal("selected SM3 not applied")
		}
	}
	if reads != 3 {
		t.Fatal("qualification cached across output batches")
	}
	roles = []string{"custom.reader"}
	untouched := &plugin.QueryResult{Rows: []map[string]any{{"phone": "13661384499"}}}
	if err := protect.Apply(untouched); err == nil || untouched.Rows[0]["phone"] != "13661384499" {
		t.Fatal("lost role passed output boundary", err)
	}
	if err := protect.Apply(&plugin.QueryResult{}); err == nil {
		t.Fatal("empty batch ignored role loss")
	}
}
