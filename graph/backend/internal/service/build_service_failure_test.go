package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/contentio"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	graphi18n "github.com/addp/graph/i18n"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/gorm"
)

type buildFixtureReader struct{}

func (buildFixtureReader) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(ref.Path[strings.LastIndex(ref.Path, "/")+1:])), nil
}
func (buildFixtureReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	return &contentio.Stat{Ref: ref, Exists: true}, nil
}

// Runs the real Graph service and repositories; only document bytes and the
// extraction response are controlled. Native graph writes remain provider-owned.
func runBuildFailureFixture(t *testing.T, db *gorm.DB, graphID uint, data *GraphDataService, extract http.HandlerFunc) (models.BuildTask, commonExecution.TaskExecution, []models.BuildMaterial) {
	t.Helper()
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(extract)
	defer server.Close()
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if data == nil {
		provider, err := plugin.Get("falkordb")
		if err != nil {
			t.Fatal(err)
		}
		caps, err := plugin.MarshalEngineCapabilities(provider.Capabilities())
		if err != nil {
			t.Fatal(err)
		}
		capValue := commonmodels.JSONString(caps)
		engine := commonmodels.Engine{Version: 1, ID: 13, Name: "fixture", EngineType: "falkordb", Capabilities: &capValue, LifecycleState: commonmodels.EngineLifecycleActive, ConnectionStatus: commonmodels.EngineConnectionOnline}
		system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(engine) }))
		t.Cleanup(system.Close)
		if err := db.Exec("ATTACH DATABASE ':memory:' AS graph").Error; err != nil {
			t.Fatal(err)
		}
		store, err := projectionstore.Migrate(db, "graph", "graph", nil)
		if err != nil {
			t.Fatal(err)
		}
		syncGraphProtection(t, store, 7, "")
		data = NewGraphDataService(repository.NewKnowledgeGraphRepository(db), repository.NewOntologyRepository(db), commonClient.NewSystemServiceClient(system.URL, graphServiceToken{}, system.Client()), NewGraphProtectionGate(store))
	}
	buildRepo := repository.NewBuildRepository(db)
	ontologyRepo := repository.NewOntologyRepository(db)
	service := &BuildService{
		buildRepo: buildRepo, graphRepo: repository.NewKnowledgeGraphRepository(db), ontologyRepo: ontologyRepo,
		ontologySvc:       NewOntologyService(ontologyRepo, repository.NewEntityTypeRepository(db), nil, nil),
		taskExecutionRepo: commonExecution.NewTaskExecutionRepository(db), graphDataSvc: data,
		materialReader: buildFixtureReader{}, copilotURL: server.URL, httpClient: server.Client(),
		serviceTokenSource: commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
			if tenant != 7 {
				t.Errorf("extraction crossed tenant: %d", tenant)
			}
			return "build-fixture-token", nil
		}),
	}
	task := models.BuildTask{TenantID: 7, GraphID: graphID, Name: "failure-fixture", ConfidenceThreshold: .7, ChunkSize: 1000, ChunkOverlap: 200, Stats: []byte(`{}`)}
	if err := buildRepo.CreateTask(&task); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bad", "good"} {
		material := models.BuildMaterial{TenantID: 7, TaskID: task.ID, GraphID: graphID, Type: "document", FileName: name, FilePath: "build/" + name, Stats: []byte(`{}`)}
		if err := buildRepo.CreateMaterial(context.Background(), &material); err != nil {
			t.Fatal(err)
		}
	}
	ctx := commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", nil))
	executionID, err := service.RunTaskWithContext(ctx, task.ID, graphID, 7, 7, commonExecution.TriggerTypeManual, commonExecution.ModuleGraph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.superviseOnce(ctx, "fixture", BuildSupervisorOptions{LeaseDuration: 2 * time.Minute, Heartbeat: 20 * time.Second, Poll: time.Second}); err != nil {
		t.Fatal(err)
	}
	var execution commonExecution.TaskExecution

	stored, err := buildRepo.GetTask(task.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where("execution_id = ?", executionID).First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	materials, err := buildRepo.ListMaterials(task.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	return *stored, execution, materials
}

func TestBuildExecutionPreservesMaterialFailuresAndContinues(t *testing.T) {
	for _, failure := range []string{"extraction", "review insert", "relation review insert", "none"} {
		t.Run(failure, func(t *testing.T) {
			db := setupGraphCleanupTestDB(t)
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			ontology := models.Ontology{TenantID: 7, Name: "build-fixture"}
			if err := db.Create(&ontology).Error; err != nil {
				t.Fatal(err)
			}
			entity := models.EntityType{TenantID: 7, OntologyID: ontology.ID, Name: "Person", Properties: []byte(`[{"name":"key","data_type":"string","unique":true}]`)}
			if err := db.Create(&entity).Error; err != nil {
				t.Fatal(err)
			}
			if failure == "relation review insert" {
				relation := models.RelationType{TenantID: 7, OntologyID: ontology.ID, Name: "Link", SourceTypeID: &entity.ID, TargetTypeID: &entity.ID}
				if err := db.Create(&relation).Error; err != nil {
					t.Fatal(err)
				}
			}
			graph := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 13, Name: "build-fixture", CatalogPath: plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 13, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "graph", Kind: "graph", Name: "fixture"}}}, IndexManagement: models.GraphIndexesManaged}
			if err := db.Create(&graph).Error; err != nil {
				t.Fatal(err)
			}
			marker := "fixture " + failure + " failure"
			if failure == "review insert" || failure == "relation review insert" {
				if err := db.Callback().Create().Before("gorm:create").Register("fixture:review-insert-failure", func(tx *gorm.DB) {
					if item, ok := tx.Statement.Dest.(*models.ReviewItem); ok {
						if (failure == "review insert" && strings.Contains(string(item.Content), `"bad"`)) ||
							(failure == "relation review insert" && item.ItemType == models.ReviewItemRelation && item.SourceText == "bad") {
							tx.AddError(errors.New(marker))
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			var calls []string
			task, execution, materials := runBuildFailureFixture(t, db, graph.ID, nil, func(w http.ResponseWriter, r *http.Request) {
				var req copilotExtractRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || r.Header.Get("Authorization") != "Bearer build-fixture-token" {
					t.Errorf("unexpected extraction request: %v", err)
					w.WriteHeader(400)
					return
				}
				calls = append(calls, req.Text)
				if failure == "extraction" && req.Text == "bad" {
					http.Error(w, marker, 500)
					return
				}
				response := copilotExtractResponse{Entities: []extractedEntity{{Type: "Person", Confidence: .1, Properties: map[string]interface{}{"key": req.Text}}}}
				if failure == "relation review insert" {
					response.Entities = []extractedEntity{
						{TempID: "source", Type: "Person", Confidence: .1, Properties: map[string]interface{}{"key": req.Text + "-source"}},
						{TempID: "target", Type: "Person", Confidence: .1, Properties: map[string]interface{}{"key": req.Text + "-target"}},
					}
					response.Relations = []extractedRelation{{Type: "Link", SourceTempID: "source", TargetTempID: "target", Confidence: .1, SourceText: req.Text}}
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			wantStatus, wantQueued := models.BuildStatusFailed, 1
			if failure == "none" {
				wantStatus, wantQueued = models.BuildStatusSuccess, 2
			} else if failure == "relation review insert" {
				wantQueued = 5 // Two queued entities in the failed material, then all three items in the next.
			}
			if task.Status != wantStatus || execution.Status != wantStatus || task.CompletedAt == nil || execution.CompletedAt == nil {
				t.Fatalf("material failure was reported as success or left active: task=%s execution=%s", task.Status, execution.Status)
			}
			if failure == "extraction" {
				wantQueued = 0
				if strings.Join(calls, ",") != "bad" || len(materials) != 2 || materials[1].Status != models.BuildMaterialStatusFailed || materials[1].ProcessedChunks != 0 {
					t.Fatalf("unknown external outcome allowed another request: calls=%v materials=%+v", calls, materials)
				}
			} else if strings.Join(calls, ",") != "bad,good" || len(materials) != 2 || materials[1].Status != models.BuildMaterialStatusCompleted || materials[1].ProcessedChunks != 1 {
				t.Fatalf("known local failure blocked later material: calls=%v materials=%+v", calls, materials)
			}
			var stats models.BuildTaskStats
			if err := json.Unmarshal(task.Stats, &stats); err != nil || stats.Processed != 2 || stats.PendingReview != wantQueued || execution.RecordsWritten == nil || *execution.RecordsWritten != 0 {
				t.Fatalf("successful work statistics were lost: %+v execution=%+v error=%v", stats, execution, err)
			}
			var queued int64
			if err := db.Model(&models.ReviewItem{}).Where("task_id = ?", task.ID).Count(&queued).Error; err != nil || queued != int64(wantQueued) {
				t.Fatalf("queued items did not match successful-work statistics: %d error=%v", queued, err)
			}
			if failure != "none" {
				if materials[0].Status != models.BuildMaterialStatusFailed || materials[0].ProcessedChunks != 0 || !strings.Contains(materials[0].ErrorMessage, marker) || !strings.Contains(task.ErrorMessage, marker) || !strings.Contains(fmt.Sprint(execution.ErrorDetails["message"]), marker) {
					t.Fatalf("original failure or failed chunk position lost: task=%+v material=%+v error=%v", task, materials[0], execution.ErrorDetails)
				}
			}
		})
	}
}

func TestBuildExtractionRejectsUnprovenIdentityAndUnknownTypes(t *testing.T) {
	for _, invalid := range []string{"unknown entity", "no unique key", "multiple unique keys", "missing key", "null key", "blank key", "unknown relation"} {
		t.Run(invalid, func(t *testing.T) {
			db := setupGraphCleanupTestDB(t)
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			ontology := models.Ontology{TenantID: 7, Name: "identity-fixture"}
			if err := db.Create(&ontology).Error; err != nil {
				t.Fatal(err)
			}
			properties := `[{"name":"key","data_type":"string","unique":true}]`
			for name, props := range map[string]string{
				"Person":   properties,
				"NoKey":    `[{"name":"name","data_type":"string"}]`,
				"ManyKeys": `[{"name":"key","data_type":"string","unique":true},{"name":"email","data_type":"string","unique":true}]`,
			} {
				entity := models.EntityType{TenantID: 7, OntologyID: ontology.ID, Name: name, Properties: []byte(props)}
				if err := db.Create(&entity).Error; err != nil {
					t.Fatal(err)
				}
			}
			graph := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 13, Name: "identity-fixture", CatalogPath: plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 13, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "graph", Kind: "graph", Name: "fixture"}}}, IndexManagement: models.GraphIndexesManaged}
			if err := db.Create(&graph).Error; err != nil {
				t.Fatal(err)
			}
			task, execution, materials := runBuildFailureFixture(t, db, graph.ID, nil, func(w http.ResponseWriter, r *http.Request) {
				var request copilotExtractRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				entity := extractedEntity{Type: "Person", Confidence: .1, Properties: map[string]interface{}{"key": request.Text}}
				response := copilotExtractResponse{}
				if request.Text == "bad" {
					switch invalid {
					case "unknown entity":
						entity.Type = "Unknown"
					case "no unique key":
						entity.Type = "NoKey"
						entity.Properties = map[string]interface{}{"name": "bad"}
					case "multiple unique keys":
						entity.Type = "ManyKeys"
					case "missing key":
						entity.Properties = map[string]interface{}{"name": "bad"}
					case "null key":
						entity.Properties["key"] = nil
					case "blank key":
						entity.Properties["key"] = " \t\n"
					case "unknown relation":
						response.Relations = []extractedRelation{{Type: "Unknown", Confidence: .1}}
					}
				}
				response.Entities = []extractedEntity{entity}
				_ = json.NewEncoder(w).Encode(response)
			})
			if task.Status != models.BuildStatusFailed || execution.Status != models.BuildStatusFailed || len(materials) != 2 || materials[0].Status != models.BuildMaterialStatusFailed || materials[0].ProcessedChunks != 0 || materials[1].Status != models.BuildMaterialStatusCompleted {
				t.Fatalf("invalid extraction was silently accepted or stopped later work: task=%s execution=%s materials=%+v", task.Status, execution.Status, materials)
			}
			var queued int64
			if err := db.Model(&models.ReviewItem{}).Where("task_id = ?", task.ID).Count(&queued).Error; err != nil || queued != 1 {
				t.Fatalf("invalid chunk reached review persistence: count=%d error=%v", queued, err)
			}
		})
	}
}

func TestBuildIdentityPreservesNonemptyScalarValuesAndLocalizedDiagnostics(t *testing.T) {
	ontology := &ontologySchemaDTO{EntityTypes: []entityTypeDTO{{Name: "Person", Properties: []propertyDTO{{Name: "key", Unique: true}}}}}
	for _, value := range []interface{}{0, false, " identifier "} {
		result := &copilotExtractResponse{Entities: []extractedEntity{{Type: "Person", Properties: map[string]interface{}{"key": value}}}}
		fields, err := validateBuildExtraction(result, ontology)
		if err != nil || fields["Person"] != "key" || result.Entities[0].Properties["key"] != value {
			t.Fatalf("valid scalar identity was rejected or normalized: value=%v fields=%v error=%v", value, fields, err)
		}
	}
	for _, code := range []string{graphi18n.MsgBuildEntityTypeUnknown, graphi18n.MsgBuildMergeKeyInvalid, graphi18n.MsgBuildMergeKeyEmpty, graphi18n.MsgBuildRelationTypeUnknown} {
		zh, en := commoni18n.ForLanguage(commoni18n.LangZhCN, code), commoni18n.ForLanguage(commoni18n.LangEn, code)
		if zh == code || en == code || zh == en {
			t.Fatalf("validation diagnostic lacks its owner translation: %s", code)
		}
	}
}
