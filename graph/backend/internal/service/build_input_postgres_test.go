package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type graphPGMaterialReader struct {
	buildFixtureReader
	text  string
	opens int
}

func (r *graphPGMaterialReader) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	r.opens++
	if ref.Path != minioBucket+"/build/source.txt" {
		return nil, errors.New("unexpected material reference")
	}
	return io.NopCloser(strings.NewReader(r.text)), nil
}

func TestIntegrationPostgresGraphBuildInputRuntime(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard Graph PostgreSQL gate required")
	}
	for _, scenario := range []string{"material_at_enqueue", "catalog_at_enqueue", "ontology_snapshot_changed", "live_ontology_edited", "material_bytes_changed", "engine_version_changed"} {
		t.Run(scenario, func(t *testing.T) {
			db, task, mat, kg := graphServicePGTask(t)
			repo := repository.NewBuildRepository(db)
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(strconv.FormatUint(uint64(task.TenantID), 10), "7", nil))
			e := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(task.TenantID), Module: execution.ModuleGraph, TaskType: execution.TaskTypeKGBuild, Source: execution.ModuleGraph, Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual}
			entity := models.EntityType{TenantID: kg.TenantID, OntologyID: kg.OntologyID, Name: "Person", NodeLabels: []byte(`["FrozenPerson"]`), Properties: []byte(`[{"name":"frozen_key","data_type":"string","unique":true}]`)}
			if err := db.Create(&entity).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Delete(&entity).Error; err != nil {
					t.Error(err)
				}
			})
			const original = "alpha"
			hash := sha256.Sum256([]byte(original))
			materials := []models.FrozenBuildMaterial{{Material: mat, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(original))}}

			// Simulate a concurrent committed change between reading the enqueue
			// inputs and entering the repository transaction.
			switch scenario {
			case "material_at_enqueue":
				if err := db.Model(&models.BuildMaterial{}).Where("id = ? AND tenant_id = ?", mat.ID, mat.TenantID).Update("file_size", mat.FileSize+1).Error; err != nil {
					t.Fatal(err)
				}
			case "catalog_at_enqueue":
				path := kg.CatalogPath
				path.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
				path.Segments[len(path.Segments)-1].Name = "changed-catalog"
				if err := db.Model(&models.KnowledgeGraph{}).Where("id = ? AND tenant_id = ?", kg.ID, kg.TenantID).Update("catalog_path", path).Error; err != nil {
					t.Fatal(err)
				}
			}
			_, _, err := repo.EnqueueExecution(ctx, task.ID, kg.ID, task.TenantID, &e, repository.BuildExecutionClaimRun, materials, 1, kg)
			if strings.HasSuffix(scenario, "_at_enqueue") {
				if !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("enqueue accepted changed input: %v", err)
				}
				for _, table := range []any{&execution.TaskExecution{}, &models.BuildExecutionControl{}} {
					var count int64
					if err := db.Model(table).Where("execution_id = ?", e.ExecutionID).Count(&count).Error; err != nil || count != 0 {
						t.Fatalf("enqueue left %T: count=%d err=%v", table, count, err)
					}
				}
				var stored models.BuildTask
				if err := db.First(&stored, task.ID).Error; err != nil || stored.ExecutionID != "" || stored.Status != models.BuildStatusPending || stored.StartedAt != nil {
					t.Fatalf("rejected enqueue changed task summary: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.DeleteMaterial(ctx, mat.ID, task.ID, task.GraphID, task.TenantID); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("queued material deletion: %v", err)
			}
			if err := repo.CheckMaterialAdmission(ctx, task.ID, task.GraphID, task.TenantID); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("queued task admitted material upload: %v", err)
			}

			var control models.BuildExecutionControl
			if err := db.First(&control, "execution_id = ?", e.ExecutionID).Error; err != nil {
				t.Fatal(err)
			}
			if scenario == "ontology_snapshot_changed" {
				var input models.BuildInput
				if err := json.Unmarshal([]byte(control.Snapshot), &input); err != nil {
					t.Fatal(err)
				}
				input.Ontology.EntityTypes[0].NodeLabels = []byte(`["ReplacedMapping"]`)
				changed, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&control).Update("snapshot", string(changed)).Error; err != nil {
					t.Fatal(err)
				}
				item, lease, _, _, err := repo.ClaimBuild(ctx, e, "g8-snapshot", time.Minute)
				if err != nil || item != nil || lease != nil {
					t.Fatalf("changed ontology snapshot produced a claim: %v", err)
				}
				var stored execution.TaskExecution
				if err := db.First(&stored, "execution_id = ?", e.ExecutionID).Error; err != nil {
					t.Fatal(err)
				}
				var summary models.BuildTask
				if err := db.First(&summary, task.ID).Error; err != nil {
					t.Fatal(err)
				}
				if stored.Status != execution.ExecutionStatusFailed || stored.Attempt != 0 || stored.StartedAt != nil ||
					stored.LeaseToken != nil || stored.ErrorDetails["code"] != "graph_input_invalid" ||
					summary.Status != models.BuildStatusFailed || summary.StartedAt != nil || summary.CompletedAt == nil {
					t.Fatal("changed ontology snapshot did not terminate as an unstarted rejection")
				}
				assertGraphBuildPGEffects(t, db, task, e.ExecutionID, 0, 0)
				return
			}

			reader := &graphPGMaterialReader{text: original}
			version := int64(1)
			switch scenario {
			case "live_ontology_edited":
				entity.NodeLabels = []byte(`["LivePerson"]`)
				entity.Properties = []byte(`[{"name":"live_key","data_type":"string","unique":true}]`)
				if err := db.Save(&entity).Error; err != nil {
					t.Fatal(err)
				}
			case "material_bytes_changed":
				reader.text = "bravo" // Same size. Only the byte identity changes.
			case "engine_version_changed":
				version++
			}
			registered, err := plugin.Get("falkordb")
			if err != nil {
				t.Fatal(err)
			}
			rawCaps, err := plugin.MarshalEngineCapabilities(registered.Capabilities())
			if err != nil {
				t.Fatal(err)
			}
			caps := commonmodels.JSONString(rawCaps)
			var copilotCalls, engineReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/system/engines/1":
					engineReads.Add(1)
					_ = json.NewEncoder(w).Encode(commonmodels.Engine{ID: kg.EngineID, Version: version, EngineType: "falkordb", Capabilities: &caps, LifecycleState: commonmodels.EngineLifecycleActive, ConnectionStatus: commonmodels.EngineConnectionOnline})
				case copilotExtractPath:
					copilotCalls.Add(1)
					var request copilotExtractRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Ontology == nil || len(request.Ontology.EntityTypes) != 1 {
						t.Error("invalid frozen ontology in Copilot request")
						http.Error(w, "invalid request", 400)
						return
					}
					person := request.Ontology.EntityTypes[0]
					if strings.Join(person.NodeLabels, ",") != "FrozenPerson" || len(person.Properties) != 1 || person.Properties[0].Name != "frozen_key" || !person.Properties[0].Unique {
						t.Error("Copilot received the edited live mapping")
					}
					_ = json.NewEncoder(w).Encode(copilotExtractResponse{Entities: []extractedEntity{{Type: "Person", Confidence: .1, Properties: map[string]interface{}{"frozen_key": original}}}})
				default:
					t.Errorf("unexpected fixture endpoint %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			graphs := repository.NewKnowledgeGraphRepository(db)
			service := &BuildService{
				buildRepo: repo, materialReader: reader, httpClient: server.Client(), copilotURL: server.URL, serviceTokenSource: graphServiceToken{},
				graphDataSvc: NewGraphDataService(graphs, repository.NewOntologyRepository(db), commonclient.NewSystemServiceClient(server.URL, graphServiceToken{}, server.Client()), nil),
			}
			item, lease, runtime, control, err := repo.ClaimBuild(ctx, e, "g8-runtime", time.Minute)
			if err != nil || item == nil || lease == nil || runtime == nil {
				t.Fatalf("claim exact fixture: %v", err)
			}
			input, err := repository.DecodeBuildInput(*item, control, *runtime)
			if err != nil {
				t.Fatal(err)
			}
			owned := execution.ContextWithLease(ctx, *lease)
			owned = context.WithValue(owned, buildInputKey{}, input)
			owned = context.WithValue(owned, effectScopeKey{}, &effectScope{repo: repo, graphID: kg.ID, tenantID: kg.TenantID})
			if err := bindEffectTarget(owned, &kg, input.EngineVersion); err != nil {
				t.Fatal(err)
			}
			service.executeTask(owned, runtime, []models.BuildMaterial{input.Materials[0].Material}, e.TenantID, e.ExecutionID)
			var terminal execution.TaskExecution
			if err := db.First(&terminal, "execution_id = ?", e.ExecutionID).Error; err != nil {
				t.Fatal(err)
			}
			var summary models.BuildTask
			if err := db.First(&summary, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if terminal.CompletedAt == nil || summary.CompletedAt == nil || terminal.Status != summary.Status {
				t.Fatal("execution and task did not reach the same terminal state")
			}
			switch scenario {
			case "live_ontology_edited":
				if terminal.Status != execution.ExecutionStatusSuccess || copilotCalls.Load() != 1 {
					t.Fatalf("frozen mapping failed: state=%s calls=%d", terminal.Status, copilotCalls.Load())
				}
				assertGraphBuildPGEffects(t, db, task, e.ExecutionID, 1, 1)
			case "material_bytes_changed":
				if terminal.Status != execution.ExecutionStatusFailed || !strings.Contains(summary.ErrorMessage, "material content identity changed") || reader.opens != 1 || copilotCalls.Load() != 0 {
					t.Fatalf("byte drift was not rejected before extraction: state=%s calls=%d", terminal.Status, copilotCalls.Load())
				}
				assertGraphBuildPGEffects(t, db, task, e.ExecutionID, 0, 0)
			case "engine_version_changed":
				if terminal.Status != execution.ExecutionStatusFailed || summary.ErrorMessage != ErrGraphTargetInvalid.Error() || engineReads.Load() != 1 || reader.opens != 0 || copilotCalls.Load() != 0 {
					t.Fatalf("registration drift was not rejected before material processing: state=%s engine_reads=%d material_opens=%d copilot_calls=%d", terminal.Status, engineReads.Load(), reader.opens, copilotCalls.Load())
				}
				assertGraphBuildPGEffects(t, db, task, e.ExecutionID, 0, 0)
			}
		})
	}
}

func assertGraphBuildPGEffects(t *testing.T, db *gorm.DB, task models.BuildTask, executionID string, operations, reviewItems int64) {
	t.Helper()
	var ops, reviews int64
	if err := db.Model(&models.GraphOperation{}).Where("execution_id = ? AND tenant_id = ?", executionID, task.TenantID).Count(&ops).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ReviewItem{}).Where("task_id = ? AND tenant_id = ?", task.ID, task.TenantID).Count(&reviews).Error; err != nil {
		t.Fatal(err)
	}
	if ops != operations || reviews != reviewItems {
		t.Fatalf("durable effects: operations=%d reviews=%d, want %d and %d", ops, reviews, operations, reviewItems)
	}
}
