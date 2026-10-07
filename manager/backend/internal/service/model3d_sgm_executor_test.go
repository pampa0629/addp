package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

type sgmObjectStore struct {
	recordingModel3DGLBObjectStore
	removed          []string
	cleanupErr       error
	cleanupCancelled bool
	expirationErr    error
	versioningStatus string
}

func (s *sgmObjectStore) GetBucketLifecycle(context.Context, string) (*lifecycle.Configuration, error) {
	return &lifecycle.Configuration{Rules: []lifecycle.Rule{{Status: "Enabled", RuleFilter: lifecycle.Filter{Prefix: "temp/"}, Expiration: lifecycle.Expiration{Days: 7}}}}, s.expirationErr
}

func (s *sgmObjectStore) GetBucketVersioning(context.Context, string) (minio.BucketVersioningConfiguration, error) {
	return minio.BucketVersioningConfiguration{Status: s.versioningStatus}, nil
}

func (s *sgmObjectStore) RemoveObject(ctx context.Context, bucket, object string, _ minio.RemoveObjectOptions) error {
	s.removed = append(s.removed, bucket+"/"+object)
	s.cleanupCancelled = ctx.Err() != nil
	return s.cleanupErr
}

func TestSGMQuickViewPipelineAndCleanup(t *testing.T) {
	for _, scenario := range []string{"success", "sgm_failure", "glb_failure", "missing_sgm", "missing_glb", "cleanup_failure", "cancelled", "lifecycle_failure", "versioning_enabled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			var firstTarget, secondSource map[string]interface{}
			makeRuntime := func(operator string, id uint) (*httptest.Server, commonModels.Engine) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/api/operators" {
						modes := []string{"workflow", "direct"}
						if (scenario == "missing_sgm" && operator == "sgm_to_osgb") || (scenario == "missing_glb" && operator == "osgb_to_glb") {
							modes = []string{"workflow"}
						}
						_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "count": 1, "operators": []map[string]interface{}{{
							"id": operator, "name": operator, "display_name": operator, "engine_type": testModel3DWorkflowEngineType,
							"category": "models", "category_path": []string{"models"}, "description": "convert", "execution_modes": modes, "effects": []string{"read", "write"},
							"parameters":   []map[string]interface{}{{"name": "access_plan", "type": "object", "required": true}},
							"output_ports": []map[string]interface{}{{"name": "default", "type": "object", "is_default": true}},
						}}})
						return
					}
					if r.URL.Path != "/api/operators/"+operator+"/invoke" {
						http.Error(w, "unexpected path", 500)
						return
					}
					var body struct {
						Params map[string]interface{} `json:"params"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
					calls = append(calls, operator)
					plan := body.Params["access_plan"].(map[string]interface{})
					if operator == "sgm_to_osgb" {
						source := plan["source"].(map[string]interface{})
						if source["kind"] != "file" || source["format"] != "sgm" {
							t.Error("SGM source identity changed")
						}
						firstTarget = plan["target"].(map[string]interface{})["access"].(map[string]interface{})
						if plan["target"].(map[string]interface{})["write_mode"] != "create" {
							t.Error("intermediate must use create")
						}
						if scenario == "cancelled" {
							cancel()
						}
					} else {
						source := plan["source"].(map[string]interface{})
						secondSource = source["access"].(map[string]interface{})
						if source["kind"] != "file" || source["format"] != "osgb" {
							t.Error("second stage must read OSGB")
						}
					}
					status := "success"
					if (operator == "sgm_to_osgb" && scenario == "sgm_failure") || (operator == "osgb_to_glb" && scenario == "glb_failure") {
						status = "failed"
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": status, "execution_id": operator + "-execution", "result": map[string]interface{}{"size_bytes": 8976}})
				}))
				u, _ := url.Parse(server.URL)
				port, _ := strconv.Atoi(u.Port())
				return server, commonModels.Engine{ID: id, Name: operator, EngineType: testModel3DWorkflowEngineType, LifecycleState: "active", ConnectionStatus: commonModels.EngineConnectionOnline, ConnectionInfo: commonModels.ConnectionInfo{"protocol": "http", "host": u.Hostname(), "port": port}, Capabilities: testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType)}
			}
			sgmServer, sgmEngine := makeRuntime("sgm_to_osgb", 98)
			defer sgmServer.Close()
			glbServer, glbEngine := makeRuntime("osgb_to_glb", 99)
			defer glbServer.Close()
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 26, "tenant_id": 7, "engine_type": "nfs", "lifecycle_state": "active", "connection_status": "online", "connection_info": map[string]interface{}{"export_path": "/mnt/addp-nfs"}})
			}))
			defer system.Close()
			store := &sgmObjectStore{recordingModel3DGLBObjectStore: recordingModel3DGLBObjectStore{statSize: 8976}}
			if scenario == "cleanup_failure" {
				store.cleanupErr = errors.New("delete rejected")
			}
			if scenario == "lifecycle_failure" {
				store.expirationErr = errors.New("policy read rejected")
			}
			if scenario == "versioning_enabled" {
				store.versioningStatus = "Enabled"
			}
			executor := NewManagerModel3DGLBExecutor(newTestSystemClient(system.URL), recordingWorkflowLister{engines: []commonModels.Engine{sgmEngine, glbEngine}}, store, "minio:9000", "ak-private", "sk-private", false, "manager", 0)
			executionID := uuid.NewString()
			result, err := executor.BuildModel3DGLB(ctx, Model3DGLBExecutionRequest{Task: &models.Model3DGLBTask{TenantID: 7}, ExecutionID: executionID, Config: Model3DGLBExecutionConfig{Source: Model3DGLBSourceConfig{ItemLocator: "addp://engine/26/path/models/compass.SGM?type=file&item_id=77", SourceEngineID: 26, ItemFingerprint: "sgm-fp", Format: "sgm"}, Result: Model3DGLBResultConfig{StorageRef: `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"model3d/preview.glb"}`, FileName: "preview.glb"}}})
			if scenario == "success" {
				if err != nil || result == nil || result.SizeBytes != 8976 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				data, _ := json.Marshal(result.Metadata)
				if strings.Contains(string(data), "ak-private") || strings.Contains(string(data), "sk-private") {
					t.Fatal("credentials leaked to metadata")
				}
				if result.Metadata["sgm_conversion"] == nil {
					t.Fatal("first runtime execution not recorded")
				}
			} else if err == nil || result != nil {
				t.Fatalf("failure must not return result: %+v %v", result, err)
			}
			if strings.HasPrefix(scenario, "missing_") || scenario == "lifecycle_failure" || scenario == "versioning_enabled" {
				if len(calls) != 0 || len(store.removed) != 0 {
					t.Fatal("failed preflight must not invoke conversion or delete any object")
				}
				return
			}
			expected := "manager/temp/model3d-sgm/tenant_7/" + executionID + "/model.osgb"
			if len(store.removed) != 1 || store.removed[0] != expected || store.cleanupCancelled {
				t.Fatalf("cleanup=%v cancelled=%v", store.removed, store.cleanupCancelled)
			}
			if scenario == "sgm_failure" || scenario == "cancelled" {
				if len(calls) != 1 {
					t.Fatalf("second stage ran after first failure: %v", calls)
				}
				return
			}
			if len(calls) != 2 || calls[0] != "sgm_to_osgb" || calls[1] != "osgb_to_glb" {
				t.Fatalf("stage order=%v", calls)
			}
			if firstTarget["bucket"] != "manager" || firstTarget["object"] != secondSource["object"] || firstTarget["endpoint"] != secondSource["endpoint"] {
				t.Fatal("stages do not share the same infra OSGB")
			}
		})
	}
}

// Exercise the actual MinIO SDK's XML contract. Policy verification is read-only
// and must reject filters which leave some late uploads outside expiration.
func TestSGMTemporaryExpirationMinIOContract(t *testing.T) {
	for _, scenario := range []string{"valid", "other_rules", "missing", "disabled", "wrong_prefix", "wrong_days", "tagged", "and_filter", "size_less", "size_greater", "fixed_date", "versioned", "suspended", "versioning_denied", "lifecycle_denied", "lifecycle_absent"} {
		t.Run(scenario, func(t *testing.T) {
			rule := lifecycle.Rule{ID: "infra-temp", Status: "Enabled", RuleFilter: lifecycle.Filter{Prefix: "temp/"}, Expiration: lifecycle.Expiration{Days: 7}}
			versioning := minio.BucketVersioningConfiguration{}
			switch scenario {
			case "disabled":
				rule.Status = "Disabled"
			case "wrong_prefix":
				rule.RuleFilter.Prefix = "tenant_7/model3d-quick-view/tmp/"
			case "wrong_days":
				rule.Expiration.Days = 30
			case "tagged":
				rule.RuleFilter.Tag = lifecycle.Tag{Key: "cleanup", Value: "yes"}
			case "and_filter":
				rule.RuleFilter.And = lifecycle.And{Prefix: "temp/", Tags: []lifecycle.Tag{{Key: "cleanup", Value: "yes"}}}
			case "size_less":
				rule.RuleFilter.And = lifecycle.And{Prefix: "temp/", ObjectSizeLessThan: 1000}
			case "size_greater":
				rule.RuleFilter.And = lifecycle.And{Prefix: "temp/", ObjectSizeGreaterThan: 1000}
			case "fixed_date":
				rule.Expiration = lifecycle.Expiration{Date: lifecycle.ExpirationDate{Time: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}}
			case "versioned":
				versioning.Status = "Enabled"
			case "suspended":
				versioning.Status = "Suspended"
			}
			config := lifecycle.Configuration{Rules: []lifecycle.Rule{rule}}
			if scenario == "missing" {
				config.Rules = nil
			}
			if scenario == "other_rules" {
				config.Rules = append(config.Rules, lifecycle.Rule{ID: "another-owner", Status: "Enabled", RuleFilter: lifecycle.Filter{Prefix: "exports/"}, Expiration: lifecycle.Expiration{Days: 30}})
			}
			var reads []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				if r.Method != http.MethodGet || r.URL.Path != "/manager-private/" {
					t.Errorf("policy verification must only read the precise bucket: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				if r.URL.Query().Has("versioning") {
					reads = append(reads, "versioning")
					if scenario == "versioning_denied" {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code></Error>"))
						return
					}
					_ = xml.NewEncoder(w).Encode(versioning)
					return
				}
				if r.URL.Query().Has("lifecycle") {
					reads = append(reads, "lifecycle")
					if scenario == "lifecycle_denied" || scenario == "lifecycle_absent" {
						code := "AccessDenied"
						status := http.StatusForbidden
						if scenario == "lifecycle_absent" {
							code = "NoSuchLifecycleConfiguration"
							status = http.StatusNotFound
						}
						w.WriteHeader(status)
						_, _ = w.Write([]byte("<Error><Code>" + code + "</Code></Error>"))
						return
					}
					_ = xml.NewEncoder(w).Encode(config)
					return
				}
				t.Errorf("unexpected policy query: %s", r.URL)
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test-key", "test-secret", ""), Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			executor := &ManagerModel3DGLBExecutor{objectStore: client}
			err = executor.verifySGMTemporaryExpiration(context.Background(), "manager-private")
			wantValid := scenario == "valid" || scenario == "other_rules"
			if (err == nil) != wantValid {
				t.Fatalf("policy verification err=%v wantValid=%v reads=%v", err, wantValid, reads)
			}
			if len(reads) == 0 || reads[0] != "versioning" {
				t.Fatalf("bucket versioning not verified first: %v", reads)
			}
		})
	}
}

func TestSGMSourceClassification(t *testing.T) {
	attrs := map[string]interface{}{"item": map[string]interface{}{"data_type": "model_3d", "format": "sgm", "layout": "single"}}
	if Model3DGLBSourceFromAttributes(attrs) == nil || !isModel3DGLBTaskSourceFormat(string(format.FormatSGM)) {
		t.Fatal("SGM single must be a GLB generation source")
	}
	attrs["item"].(map[string]interface{})["layout"] = "whole"
	if Model3DGLBSourceFromAttributes(attrs) != nil {
		t.Fatal("SGM whole scope is unsupported")
	}
}

func TestSGMCapabilityRequiresBothDirectOperators(t *testing.T) {
	for _, scenario := range []string{"both", "missing_sgm", "missing_glb", "workflow_only", "offline", "discovery_unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/operators" {
					http.Error(w, "capability must not invoke operators", 500)
					return
				}
				operators := []map[string]interface{}{}
				for _, name := range []string{"sgm_to_osgb", "osgb_to_glb"} {
					if (scenario == "missing_sgm" && name == "sgm_to_osgb") || (scenario == "missing_glb" && name == "osgb_to_glb") {
						continue
					}
					modes := []string{"workflow", "direct"}
					if scenario == "workflow_only" {
						modes = []string{"workflow"}
					}
					operators = append(operators, map[string]interface{}{
						"id": name, "name": name, "display_name": name, "engine_type": testModel3DWorkflowEngineType,
						"category": "models", "category_path": []string{"models"}, "description": "convert", "execution_modes": modes, "effects": []string{"read", "write"},
						"parameters":   []map[string]interface{}{{"name": "access_plan", "type": "object", "required": true}},
						"output_ports": []map[string]interface{}{{"name": "default", "type": "object", "is_default": true}},
					})
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "count": len(operators), "operators": operators})
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			engine := commonModels.Engine{ID: 99, EngineType: testModel3DWorkflowEngineType, LifecycleState: "active", ConnectionStatus: commonModels.EngineConnectionOnline, ConnectionInfo: commonModels.ConnectionInfo{"protocol": "http", "host": u.Hostname(), "port": port}, Capabilities: testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType)}
			if scenario == "offline" {
				engine.ConnectionStatus = "offline"
			}
			db := newTileCacheTaskServiceTestDB(t)
			createModel3DGLBTableForTest(t, db)
			svc := NewQuickViewService(db, nil)
			if scenario != "discovery_unavailable" {
				svc.SetWorkflowEngineLister(recordingWorkflowLister{engines: []commonModels.Engine{engine}})
			}
			capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
				Identity: QuickViewIdentity{TenantID: 7, ItemFingerprint: "sgm-fp", Locator: "addp://engine/26/path/models/compass.SGM?type=file&item_id=77"},
				EngineID: 26, Model3D: &Model3DGLBSource{Format: "sgm", Layout: "single", SourceSizeBytes: 100},
			})
			if err != nil {
				t.Fatal(err)
			}
			if capability.CanUseQuickView {
				t.Fatal("unconverted SGM must not be renderable")
			}
			if got := containsString(capability.AvailableActions, QuickViewActionGenerateModel3DGLB); got != (scenario == "both") {
				t.Fatalf("generation action=%v reason=%s", got, capability.UnavailableReason)
			}
		})
	}
}
