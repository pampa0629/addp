package projectionstore

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNativeRoleChoiceProtection(t *testing.T) {
	assertNativeRoleChoiceProtection(t, openProjectionStoreDB(t), "transfer")
}

// The standard projection-store PostgreSQL gate discovers this test. It creates
// only an owned schema in the gate's existing isolated database.
func TestProjectionStoreNativeRoleChoicesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	const schema = "projection_store_native_roles_it"
	dropProjectionStoreTestSchema(t, db, schema)
	t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
	assertNativeRoleChoiceProtection(t, db, schema)
}

func assertNativeRoleChoiceProtection(t *testing.T, db *gorm.DB, schema string) {
	t.Helper()
	now := time.Now().UTC()
	store, err := Migrate(db, schema, "transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	projection := activeTableProjection(t, "transfer", "export", model, path, fields)
	option := dataprotection.RoleMaskingOption{ID: uuid.NewString(), Name: "统计哈希", RoleKeys: []string{"custom.analytics"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]any{}, InvalidValueEffect: dataprotection.EffectSuppress}}
	projection.Rules[0].RoleOptions = &dataprotection.RoleMaskingOptions{PolicyID: uuid.NewString(), AssessmentRevision: 1, PolicyRevision: 1, Options: []dataprotection.RoleMaskingOption{option}}
	publish := func(from, to string) {
		t.Helper()
		if err := projection.Seal(); err != nil {
			t.Fatal(err)
		}
		if err := store.ApplyBatch(t.Context(), 7, from, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: to, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: to}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	publish("", "native-role-1")
	if err := store.ApplyBatch(t.Context(), 8, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{}, NextCursor: "empty"}, now); err != nil {
		t.Fatal(err)
	}
	selection := &dataprotection.QueryRoleSelection{Choices: []dataprotection.FieldRoleChoice{{Target: projection.Target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: projection.Rules[0].RoleOptions.PolicyID, AssessmentRevision: 1, PolicyRevision: 1, OptionID: option.ID}}}, RoleKeys: []string{"custom.analytics"}, ExpiresAt: now.Add(time.Minute)}
	prepare := func(tenant int64, action string, choice *dataprotection.QueryRoleSelection) (*dataprotection.PreparedTableProtection, error) {
		return store.PrepareTableProtection(t.Context(), tenant, model, path, fields, action, choice, time.Now().UTC())
	}
	result := func() *plugin.QueryResult {
		return &plugin.QueryResult{Columns: []string{"phone"}, Rows: []map[string]any{{"phone": "13661384499"}, {"phone": nil}}}
	}
	unmanagedPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Other")
	if protect, err := store.PrepareTableProtection(t.Context(), 7, model, unmanagedPath, fields, "export", selection, now); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) || protect != nil {
		t.Fatal("unmanaged source silently discarded saved choice", err)
	}
	chosen, err := prepare(7, "export", selection)
	if err != nil {
		t.Fatal(err)
	}
	selected := result()
	if err := chosen.Apply(selected); err != nil {
		t.Fatal(err)
	}
	selectedValue, ok := selected.Rows[0]["phone"].(string)
	if !ok || len(selectedValue) != 64 || selected.Rows[1]["phone"] != nil {
		t.Fatal("selected SM3 or NULL semantics changed")
	}
	assertDefault := func(protect *dataprotection.PreparedTableProtection, apply func(*plugin.QueryResult) error) {
		t.Helper()
		preview := result()
		if protect == nil || apply == nil {
			t.Fatal("missing default protection")
		}
		if err := apply(preview); err != nil {
			t.Fatal(err)
		}
		if preview.Rows[0]["phone"] != "136****4499" || preview.Rows[1]["phone"] != nil {
			t.Fatal("default output changed")
		}
	}
	assertDefault(chosen, chosen.ApplyDefault)
	defaults, err := prepare(7, "export", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertDefault(defaults, defaults.Apply)
	// Adding a role only increases eligibility; it cannot change a saved method.
	selection.RoleKeys = []string{"custom.analytics", "custom.reader"}
	additional := result()
	if err := chosen.Apply(additional); err != nil || additional.Rows[0]["phone"] != selectedValue {
		t.Fatal("additional role changed method", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*dataprotection.QueryRoleSelection)
		tenant int64
		action string
	}{
		{"tenant", func(*dataprotection.QueryRoleSelection) {}, 8, "export"},
		{"outlet", func(*dataprotection.QueryRoleSelection) {}, 7, "query"},
		{"role", func(s *dataprotection.QueryRoleSelection) { s.RoleKeys = []string{"custom.reader"} }, 7, "export"},
		{"expired", func(s *dataprotection.QueryRoleSelection) { s.ExpiresAt = now }, 7, "export"},
		{"revision", func(s *dataprotection.QueryRoleSelection) { s.Choices[0].Selection.PolicyRevision++ }, 7, "export"},
		{"field", func(s *dataprotection.QueryRoleSelection) { s.Choices[0].ComponentKey = "other" }, 7, "export"},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := *selection
			invalid.Choices = append([]dataprotection.FieldRoleChoice(nil), selection.Choices...)
			test.mutate(&invalid)
			if protect, err := prepare(test.tenant, test.action, &invalid); err == nil || protect != nil {
				t.Fatal("invalid selection was silently defaulted")
			}
		})
	}
	// Empty batches must also recheck qualification, before any output mutation.
	selection.ExpiresAt = now
	empty := &plugin.QueryResult{Columns: []string{"phone"}}
	if err := chosen.Apply(empty); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatal("expired role observation accepted", err)
	}
	selection.ExpiresAt = now.Add(time.Minute)
	selection.RoleKeys = []string{"custom.reader"}
	untouched := result()
	if err := chosen.Apply(untouched); err == nil || untouched.Rows[0]["phone"] != "13661384499" {
		t.Fatal("role loss was not refused before mutation", err)
	}
	selection.RoleKeys = []string{"custom.analytics"}
	projection.Revision = "00000000000000000002"
	projection.Rules[0].RoleOptions.PolicyRevision++
	publish("native-role-1", "native-role-2")
	untouched = result()
	if err := chosen.Apply(untouched); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) || untouched.Rows[0]["phone"] != "13661384499" {
		t.Fatal("old prepared choice survived policy revision", err)
	}
	// A changed optional policy must not stop a caller using default protection.
	defaults, err = prepare(7, "export", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertDefault(defaults, defaults.Apply)
}
