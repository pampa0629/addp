package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The target does not exist in Meta yet. This is a producer-proved read plan,
// never an Allow or an assertion of current source existence/Grant eligibility.
func exerciseProductionScanPlanFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	parent := uuid.NewString()
	tenant := uint(7)
	target := plugin.TabularItemPath(9, "schema", " space.schema ", "result.with.dot")
	locator := "addp://engine/9/path/%20space.schema%20/result.with.dot?type=table"
	run := scantask.NewManualExecution(tenant, 31, 9, 0, "postgresql", []string{locator}, nil, nil, "basic", true, "transfer", time.Now())
	principal, membership, version := int64(31), int64(51), int64(6)
	run.ParentExecutionID = &parent
	run.ActorPrincipalID = &principal
	run.ActorTenantMembershipID = &membership
	run.IssuedAuthorizationVersion = &version
	run.Metadata = commonmodels.JSONMap{"initial_production_execution_id": parent, "private": "not-for-wire"}
	freezeScanEngineSnapshot(run, &commonmodels.Engine{ID: 9, TenantID: &tenant, Version: 3, EngineType: "postgresql"})
	run.ExecutionConfig["initial_production_target"] = target
	originalConfig, _ := json.Marshal(run.ExecutionConfig)
	originalMetadata, _ := json.Marshal(run.Metadata)
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	service := NewScanExecutionService(db, nil, nil, nil)
	facts, err := service.GetScanPlanFacts(t.Context(), tenant, run.ExecutionID)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.SourceScope == nil || !reflect.DeepEqual(facts.SourceScope.Anchors, []plugin.EngineCatalogPath{target}) || facts.SourceScope.Mode != "exact" {
		t.Fatalf("new output facts=%+v err=%v", facts, err)
	}
	raw, _ := json.Marshal(facts)
	for _, hidden := range []string{"initial_production_target", parent, "actor_principal_id", "not-for-wire"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatalf("private evidence leaked: %s", hidden)
		}
	}
	originalDigest := facts.ExecutionPlanDigest
	cases := []struct {
		name   string
		mutate func(map[string]any, map[string]any) map[string]any
	}{
		{"missing parent", func(c, m map[string]any) map[string]any { return map[string]any{"parent_execution_id": nil} }},
		{"invalid parent", func(c, m map[string]any) map[string]any { return map[string]any{"parent_execution_id": "invalid"} }},
		{"wrong marker", func(c, m map[string]any) map[string]any {
			m["initial_production_execution_id"] = uuid.NewString()
			return nil
		}},
		{"no marker", func(c, m map[string]any) map[string]any { delete(m, "initial_production_execution_id"); return nil }},
		{"no actor tuple", func(c, m map[string]any) map[string]any {
			return map[string]any{"actor_principal_id": nil, "actor_tenant_membership_id": nil, "issued_authorization_version": nil}
		}},
		{"missing principal", func(c, m map[string]any) map[string]any { return map[string]any{"actor_principal_id": nil} }},
		{"missing membership", func(c, m map[string]any) map[string]any { return map[string]any{"actor_tenant_membership_id": nil} }},
		{"zero actor version", func(c, m map[string]any) map[string]any { return map[string]any{"issued_authorization_version": 0} }},
		{"wrong triggered user", func(c, m map[string]any) map[string]any { return map[string]any{"triggered_by": 99} }},
		{"scheduled", func(c, m map[string]any) map[string]any { return map[string]any{"trigger_type": "scheduled"} }},
		{"wrong row source", func(c, m map[string]any) map[string]any { return map[string]any{"source": "manager"} }},
		{"wrong config source", func(c, m map[string]any) map[string]any { c["source"] = "manager"; return nil }},
		{"deep", func(c, m map[string]any) map[string]any { c["scan_depth"] = "deep"; return nil }},
		{"not force", func(c, m map[string]any) map[string]any { c["force"] = false; return nil }},
		{"extra item", func(c, m map[string]any) map[string]any { c["item_id"] = 0; return nil }},
		{"extra catalog path", func(c, m map[string]any) map[string]any { c["catalog_paths"] = []string{"other"}; return nil }},
		{"extra ref", func(c, m map[string]any) map[string]any {
			c["ref_groups"] = []any{map[string]any{"refs": []string{"other"}}}
			return nil
		}},
		{"extra selector", func(c, m map[string]any) map[string]any { c["node_id"] = 1; return nil }},
		{"extra read option", func(c, m map[string]any) map[string]any { c["has_header"] = false; return nil }},
		{"null typed", func(c, m map[string]any) map[string]any { c["initial_production_target"] = nil; return nil }},
		{"bad typed kind", func(c, m map[string]any) map[string]any {
			v := target
			v.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
			v.Segments[2].Kind = "view"
			c["initial_production_target"] = v
			return nil
		}},
		{"bad typed engine", func(c, m map[string]any) map[string]any {
			v := target
			v.EngineID = 10
			c["initial_production_target"] = v
			return nil
		}},
		{"different locator", func(c, m map[string]any) map[string]any {
			c["targets"] = []string{"addp://engine/9/path/other/result.with.dot?type=table"}
			return nil
		}},
		{"no locator", func(c, m map[string]any) map[string]any { c["targets"] = []string{}; return nil }},
		{"two locators", func(c, m map[string]any) map[string]any { c["targets"] = []string{locator, locator}; return nil }},
		{"not PG", func(c, m map[string]any) map[string]any { c["engine_type"] = "mysql"; return nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var config, metadata map[string]any
			_ = json.Unmarshal(originalConfig, &config)
			_ = json.Unmarshal(originalMetadata, &metadata)
			updates := tc.mutate(config, metadata)
			if updates == nil {
				updates = map[string]any{}
			}
			updates["execution_config"] = commonmodels.JSONMap(config)
			updates["metadata"] = commonmodels.JSONMap(metadata)
			storageRejectsActor := db.Dialector.Name() == "postgres" && (tc.name == "missing principal" || tc.name == "missing membership" || tc.name == "zero actor version")
			if storageRejectsActor {
				if err := db.SavePoint("meta_production_invalid_actor").Error; err != nil {
					t.Fatal(err)
				}
			}
			updateErr := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", run.ExecutionID).Updates(updates).Error
			if storageRejectsActor {
				// Real PostgreSQL cannot persist an invalid Actor tuple. Prove the
				// storage guard and restore the transaction, rather than dropping
				// its constraint or treating later aborted queries as rejections.
				var violation interface{ SQLState() string }
				if !errors.As(updateErr, &violation) || violation.SQLState() != "23514" || !strings.Contains(updateErr.Error(), "task_executions_execution_authorization_tuple_check") {
					t.Fatalf("expected Actor storage constraint, got %v", updateErr)
				}
				if err := db.RollbackTo("meta_production_invalid_actor").Error; err != nil {
					t.Fatal(err)
				}
				unchanged, err := service.GetScanPlanFacts(t.Context(), tenant, run.ExecutionID)
				if err != nil || unchanged.BasisState != authorizationfacts.AuthorizationBasisComplete {
					t.Fatalf("rejected tuple changed committed facts: %+v err=%v", unchanged, err)
				}
				return
			}
			if updateErr != nil {
				t.Fatal(updateErr)
			}
			got, err := service.GetScanPlanFacts(t.Context(), tenant, run.ExecutionID)
			if err != nil || got.BasisState != authorizationfacts.AuthorizationBasisIncomplete || got.SourceScope != nil {
				t.Fatalf("incomplete producer widened: %+v err=%v", got, err)
			}
			// Restore the original raw row before the next independent mutation.
			var restoreConfig, restoreMetadata commonmodels.JSONMap
			_ = json.Unmarshal(originalConfig, &restoreConfig)
			_ = json.Unmarshal(originalMetadata, &restoreMetadata)
			if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", run.ExecutionID).Updates(map[string]any{"source_task_id": nil, "trigger_type": "manual", "source": "transfer", "triggered_by": 31, "actor_principal_id": principal, "actor_tenant_membership_id": membership, "issued_authorization_version": version, "parent_execution_id": parent, "execution_config": restoreConfig, "metadata": restoreMetadata}).Error; err != nil {
				t.Fatal(err)
			}
		})
	}
	// A changed private target participates in the same raw plan digest even if
	// the unaltered locator makes the plan no longer projectable.
	config := commonmodels.JSONMap{}
	_ = json.Unmarshal(originalConfig, &config)
	changed := target
	changed.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
	changed.Segments[2].Name = "different"
	config["initial_production_target"] = changed
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", run.ExecutionID).Update("execution_config", config).Error; err != nil {
		t.Fatal(err)
	}
	got, err := service.GetScanPlanFacts(t.Context(), tenant, run.ExecutionID)
	if err != nil || got.ExecutionPlanDigest == originalDigest || got.BasisState != authorizationfacts.AuthorizationBasisIncomplete {
		t.Fatalf("private target escaped plan digest: %+v %v", got, err)
	}
}

func TestInitialProductionScanPlanFacts(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createTaskExecutionTable(t, db)
	exerciseProductionScanPlanFacts(t, db)
}
