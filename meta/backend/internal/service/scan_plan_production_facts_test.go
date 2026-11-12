package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/production"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func exerciseProductionScanPlanFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, kind := range []string{"native", "file", "service_only", "large_engine"} {
		t.Run(kind, func(t *testing.T) {
			tenant := uint(7)
			engineID := uint(9)
			if kind == "large_engine" {
				engineID = 9007199254740993
			}
			target := plugin.TabularItemPath(engineID, "schema", " space.schema ", "result.with.dot")
			if kind == "file" || kind == "service_only" {
				target = plugin.FileItemPath(engineID, "out/result.gpkg")
			}
			ref := production.Reference{Owner: "develop", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}
			receipt := production.Receipt{Reference: ref, TenantID: 7, TargetEngineVersion: 3, Attempt: 1, OperationID: uuid.NewString(), CommittedAt: time.Now(), Primary: target, Objects: []production.CommittedObject{{Target: target, Created: true}}, Origin: &production.UserOrigin{PrincipalID: 31, MembershipID: 51, AuthorizationVersion: 6}, Sources: production.SourceEvidence{Kind: production.SourcesUnknown}}
			req, err := productionScanRequest(receipt)
			if err != nil {
				t.Fatal(err)
			}
			profile := "postgresql"
			if kind == "file" || kind == "service_only" {
				profile = "nfs"
			}
			run := scantask.NewManualExecution(tenant, 31, engineID, 0, profile, req.Targets, nil, req.RefGroups, "basic", true, ref.Owner, time.Now())
			p, m, v := int64(31), int64(51), int64(6)
			run.ParentExecutionID = &ref.ExecutionID
			run.ActorPrincipalID = &p
			run.ActorTenantMembershipID = &m
			run.IssuedAuthorizationVersion = &v
			if kind == "service_only" {
				receipt.Origin = nil
				run.TriggeredBy = nil
				run.ActorPrincipalID = nil
				run.ActorTenantMembershipID = nil
				run.IssuedAuthorizationVersion = nil
			}
			freezeScanEngineSnapshot(run, &commonmodels.Engine{ID: engineID, TenantID: &tenant, Version: 3, EngineType: profile})
			run.ExecutionConfig["initial_production_receipt"] = receipt
			run.ExecutionConfig["initial_production_new_candidate"] = true
			if err := db.Create(run).Error; err != nil {
				t.Fatal(err)
			}
			svc := NewScanExecutionService(db, nil, nil, nil)
			precise, err := findProductionBasicScan(db, tenant, ref)
			if err != nil {
				t.Fatal(err)
			}
			if proof, err := frozenProductionReceipt(precise); err != nil || proof.Primary.EngineID != engineID {
				t.Fatal("authoritative execution JSON rounded Engine ID", proof, err)
			}

			facts, err := svc.GetScanPlanFacts(t.Context(), tenant, run.ExecutionID)
			if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.SourceScope == nil || len(facts.SourceScope.Anchors) != 1 {
				t.Fatalf("exact production facts=%+v err=%v", facts, err)
			}
			wire, _ := json.Marshal(facts)
			for _, private := range []string{"initial_production_receipt", ref.ExecutionID, "actor_principal_id", "origin"} {
				if strings.Contains(string(wire), private) {
					t.Fatal("private facts leaked", private)
				}
			}
			original, _ := json.Marshal(run.ExecutionConfig)
			for _, scenario := range []string{"wrong_parent", "foreign_tenant", "wrong_actor", "extra_selector", "extra_target", "engine_version", "source", "depth", "missing_receipt"} {
				t.Run(scenario, func(t *testing.T) {
					var config commonmodels.JSONMap
					if json.Unmarshal(original, &config) != nil {
						t.Fatal("fixture")
					}
					updates := map[string]any{}
					switch scenario {
					case "wrong_parent":
						updates["parent_execution_id"] = uuid.NewString()
					case "foreign_tenant":
						config["initial_production_receipt"].(map[string]any)["tenant_id"] = "8"
					case "wrong_actor":
						updates["actor_principal_id"] = 88
						updates["actor_tenant_membership_id"] = 51
						updates["issued_authorization_version"] = 6
						updates["triggered_by"] = 88
					case "extra_selector":
						config["item_id"] = 3
					case "extra_target":
						config["targets"] = []string{"addp://engine/9/path/public/sibling?type=table"}
					case "engine_version":
						config["engine_version"] = "4"
					case "source":
						config["source"] = "manager"
						updates["source"] = "manager"
					case "depth":
						config["scan_depth"] = "deep"
					case "missing_receipt":
						delete(config, "initial_production_receipt")
					}
					updates["execution_config"] = config
					if err := db.Model(&execution.TaskExecution{}).Where("id=?", run.ID).Updates(updates).Error; err != nil {
						t.Fatal(err)
					}
					observed, err := svc.GetScanPlanFacts(t.Context(), tenant, run.ExecutionID)
					if err == nil && observed.BasisState == authorizationfacts.AuthorizationBasisComplete {
						t.Fatal("mutated production plan projected trusted scope", scenario)
					}
					if err := db.Model(&execution.TaskExecution{}).Where("id=?", run.ID).Updates(map[string]any{"execution_config": run.ExecutionConfig, "parent_execution_id": run.ParentExecutionID, "actor_principal_id": run.ActorPrincipalID, "actor_tenant_membership_id": run.ActorTenantMembershipID, "issued_authorization_version": run.IssuedAuthorizationVersion, "triggered_by": run.TriggeredBy, "source": run.Source}).Error; err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
func TestInitialProductionScanPlanFacts(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	exerciseProductionScanPlanFacts(t, db)
}
