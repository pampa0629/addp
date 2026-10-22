package projectionstore

import (
	"context"
	"errors"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"testing"
	"time"
)

type choiceNativePlan struct {
	plugin.PreparedQuery
	bound int
}

func (p *choiceNativePlan) BindOutputTransforms(_ context.Context, ops []plugin.QueryOutputTransform) error {
	p.bound += len(ops)
	return nil
}

func TestRoleChoiceRequiresCurrentQualificationExactLineageAndSeparateHistory(t *testing.T) {
	for _, kind := range []string{"identity", "alias", "unused", "derived", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now().UTC()
			model := plugin.DynamicSchemaCatalogModel()
			path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
			fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
			projection := activeTableProjection(t, "develop", "query", model, path, fields)
			option := dataprotection.RoleMaskingOption{ID: uuid.NewString(), Name: "统计哈希", RoleKeys: []string{"custom.analytics"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]any{}, InvalidValueEffect: dataprotection.EffectSuppress}}
			projection.Rules[0].RoleOptions = &dataprotection.RoleMaskingOptions{PolicyID: uuid.NewString(), AssessmentRevision: 1, PolicyRevision: 1, Options: []dataprotection.RoleMaskingOption{option}}
			if err := projection.Seal(); err != nil {
				t.Fatal(err)
			}
			store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "roles", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "roles"}, now); err != nil {
				t.Fatal(err)
			}
			selection := &dataprotection.QueryRoleSelection{Choices: []dataprotection.FieldRoleChoice{{Target: projection.Target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: projection.Rules[0].RoleOptions.PolicyID, AssessmentRevision: 1, PolicyRevision: 1, OptionID: option.ID}}}, RoleKeys: []string{"custom.analytics", "custom.reader"}, ExpiresAt: now.Add(time.Minute)}
			for _, roles := range [][]string{{"custom.analytics"}, {"custom.analytics", "custom.reader"}, {"custom.reader"}} {
				candidates, err := store.RoleOptions(t.Context(), 7, projection.Target, "query", roles, now)
				if err != nil || len(candidates) != 1 || (len(candidates[0].Options) == 1) != (roles[0] == "custom.analytics") {
					t.Fatal("role union changed", err)
				}
			}
			source := plugin.QueryOutputSource{Path: path, Fields: fields, IdentityOutput: kind == "identity"}
			output := "phone"
			if kind != "identity" {
				output = "contact"
			}
			if kind == "alias" || kind == "derived" || kind == "mixed" {
				source.Bindings = []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{output}, Transformation: plugin.QueryOutputTransformationDirect}}
				if kind == "mixed" {
					source.Bindings = append(source.Bindings, plugin.QueryOutputBinding{SourcePath: []string{"phone"}, OutputPath: []string{"derived"}, Transformation: plugin.QueryOutputTransformationDerived})
				}
				if kind == "derived" {
					source.Bindings[0].Transformation = plugin.QueryOutputTransformationDerived
				}
			}
			makePlan := func() *choiceNativePlan {
				plan, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil, func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) }, func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
					return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{source}}, nil
				}, func(context.Context) (*plugin.QueryResult, error) {
					return &plugin.QueryResult{Columns: []string{output}, Rows: []map[string]interface{}{{output: "13661384499"}}}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				return &choiceNativePlan{PreparedQuery: plan}
			}
			actor := dataprotection.SubjectReference{Type: "user", ID: "41"}
			plan := makePlan()
			protect, err := store.PrepareQueryProtection(t.Context(), 7, model, plan, "query", actor, selection, now)
			if kind == "unused" || kind == "derived" || kind == "mixed" {
				if err == nil || protect != nil {
					t.Fatal("unconsumed or derived selection accepted")
				}
				return
			}
			if err != nil || plan.bound != 0 {
				t.Fatal("selected field was transformed natively before dual protection", err)
			}
			result, err := plan.Execute(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			captured, err := protect.CaptureAuthorized(result, actor)
			if err != nil || captured == nil {
				t.Fatal(err)
			}
			if captured.Result.Rows[0][output] == "13661384499" || len(captured.Sources[0].Source.Bindings) > 0 {
				t.Fatal("selected mask retained raw output")
			}
			if err := protect.ApplyDefault(result); err != nil {
				t.Fatal(err)
			}
			if result.Rows[0][output] == captured.Result.Rows[0][output] || result.Rows[0][output] == "13661384499" {
				t.Fatal("history did not retain independent default mask")
			}
			if _, err := store.ReadAuthorizedQueryResult(t.Context(), 7, captured, actor, "query", selection); err != nil {
				t.Fatal(err)
			}
			revoked := *selection
			revoked.RoleKeys = []string{"custom.reader"}
			if _, err := store.ReadAuthorizedQueryResult(t.Context(), 7, captured, actor, "query", &revoked); err == nil {
				t.Fatal("revoked role accepted")
			}
			expired := *selection
			expired.ExpiresAt = now
			if _, err := store.PrepareQueryProtection(t.Context(), 7, model, makePlan(), "query", actor, &expired, now); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
				t.Fatal("expired observation accepted", err)
			}
			stale := *selection
			stale.Choices = append([]dataprotection.FieldRoleChoice(nil), selection.Choices...)
			stale.Choices[0].Selection.PolicyRevision = 2
			if err := store.ValidateRoleChoices(t.Context(), 7, &stale, "query", now); err == nil {
				t.Fatal("stale revision accepted")
			}
		})
	}
}
