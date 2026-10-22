package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	ftpPlugin "github.com/addp/common/engine/plugins/ftp"
	_ "github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type ftpModelReadSpy struct {
	*ftpPlugin.FTPPlugin
	engineType   string
	mu           sync.Mutex
	t            *testing.T
	files        map[string][]byte
	facts, opens []string
	closes       int
}

func (s *ftpModelReadSpy) Type() string { return s.engineType }

func (s *ftpModelReadSpy) DescribeEngineCatalogFacts(_ context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	s.checkConnection(info)
	name := target.StringPath()
	s.mu.Lock()
	s.facts = append(s.facts, name)
	s.mu.Unlock()
	data, ok := s.files[name]
	if !ok {
		return nil, errors.New("exact model file absent")
	}
	size := int64(len(data))
	return &plugin.EngineCatalogFacts{Path: target, Kind: plugin.EngineCatalogKindFile, Storage: &plugin.EngineCatalogStorageFacts{Path: name, SizeBytes: &size}}, nil
}
func (s *ftpModelReadSpy) OpenContent(_ context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	s.checkConnection(info)
	name := target.StringPath()
	s.mu.Lock()
	s.opens = append(s.opens, name)
	s.mu.Unlock()
	data, ok := s.files[name]
	if !ok {
		return nil, errors.New("exact model file absent")
	}
	return &ftpModelSpyReader{Reader: bytes.NewReader(data), closed: func() { s.mu.Lock(); s.closes++; s.mu.Unlock() }}, nil
}
func (s *ftpModelReadSpy) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	s.t.Error("model source attempted directory enumeration")
	return nil, errors.New("directory enumeration forbidden")
}
func (s *ftpModelReadSpy) readCounts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.facts), len(s.opens)
}

func (s *ftpModelReadSpy) checkConnection(info plugin.ConnectionInfo) {
	s.t.Helper()
	if info["password"] != "current-consumption-connection" {
		s.t.Fatal("source did not use same-consumption Engine connection")
	}
}

type ftpModelSpyReader struct {
	*bytes.Reader
	closed func()
}

func (r *ftpModelSpyReader) Close() error { r.closed(); return nil }

type ftpModelServiceTokens struct{}

func (ftpModelServiceTokens) Token(_ context.Context, tenant uint) (string, error) {
	if tenant != 7 {
		return "", errors.New("wrong tenant")
	}
	return "addp_at_model3d_manager_service", nil
}
func (ftpModelServiceTokens) PlatformToken(context.Context) (string, error) {
	return "", errors.New("platform credential forbidden")
}

func fileModelAuthorizedFixture(t *testing.T, engineType, format string, denied func(*ftpModelReadSpy) bool) (context.Context, Model3DGLBExecutionRequest, *commonModels.Engine, *ftpModelReadSpy, *atomic.Int32) {
	t.Helper()
	source, item := model3DGLBCandidateFixture(format)
	item.Fingerprint = commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	source.ItemFingerprint = item.Fingerprint
	candidate, err := model3DGLBCandidateFromMeta(7, source, item)
	if err != nil {
		t.Fatal(err)
	}
	spy := &ftpModelReadSpy{FTPPlugin: &ftpPlugin.FTPPlugin{}, engineType: engineType, t: t, files: map[string][]byte{item.FullName: []byte("ifc input")}}
	if format == "3ds" {
		spy.files[item.FullName] = threeDSStagingInput("TEST.PNG")
		spy.files["models/TEST.PNG"] = []byte("actual texture")
	}
	original, err := plugin.Get(engineType)
	if err != nil {
		t.Fatal(err)
	}
	plugin.Unregister(engineType)
	plugin.Register(spy)
	t.Cleanup(func() { plugin.Unregister(engineType); plugin.Register(original) })
	tenant := uint(7)
	engine := &commonModels.Engine{ID: 33, TenantID: &tenant, Version: 5, EngineType: engineType, LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: commonModels.ConnectionInfo{"host": "127.0.0.1", "port": 2121, "user": "readonly", "security": "plain", "password": "classification-must-not-be-used"}}
	calls := &atomic.Int32{}
	var expectedScope *commonExecution.ManagerModel3DReadScope
	var expectedExecution, expectedToken string
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/system/execution-authorizations/51/manager-model3d-glb-accesses" || r.Header.Get("Authorization") != "Bearer addp_at_model3d_manager_service" {
			t.Error("unexpected authorization protocol or identity")
			w.WriteHeader(403)
			return
		}
		var body commonClient.ManagerModel3DAccessRequest
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.ExecutionID != expectedExecution || body.Attempt != 1 || body.LeaseToken != expectedToken || !reflect.DeepEqual(&body.SourceReadScope, expectedScope) {
			t.Error("source consumer changed the frozen execution scope")
			w.WriteHeader(403)
			return
		}
		if denied != nil && denied(spy) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(403)
			io.WriteString(w, `{"error_code":"forbidden","message":"revoked"}`)
			return
		}
		current := *engine
		current.ConnectionInfo = commonModels.ConnectionInfo{"host": "127.0.0.1", "port": 2121, "user": "readonly", "security": "plain", "password": "current-consumption-connection"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"observed_at": time.Now().UTC(), "engine": &current})
	}))
	t.Cleanup(system.Close)
	executionID, token, owner := uuid.NewString(), uuid.NewString(), "model3d-worker"
	expires := time.Now().Add(time.Hour)
	principal, membership, version, authorization := int64(31), int64(41), int64(1), int64(51)
	actor := 31
	execution := &commonExecution.TaskExecution{ExecutionID: executionID, TenantID: 7, Module: commonExecution.ModuleManager, TaskType: commonExecution.TaskTypeModel3DGLBGeneration, Source: commonExecution.ModuleManager, TriggerType: commonExecution.TriggerTypeManual, SourceTaskID: commonExecution.NewSourceTaskIDFromUint(17), ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, Status: commonExecution.ExecutionStatusRunning, Attempt: 1, TriggeredBy: &actor, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorization, AuthorizationExpiresAt: &expires, LeaseExpiresAt: &expires, LeaseToken: &token, LeaseOwner: &owner}
	ctx := commonExecution.ContextWithLease(t.Context(), commonExecution.Lease{ExecutionID: executionID, TenantID: 7, Attempt: 1, Token: token, Owner: owner})
	raw, _ := json.Marshal(map[string]any{"source": map[string]any{"item_locator": source.ItemLocator, "source_engine_id": source.SourceEngineID, "item_id": source.ItemID, "item_fingerprint": source.ItemFingerprint, "format": source.Format}, "result": map[string]any{"storage_ref": "previous-owned", "file_name": "model.glb"}, "options": map[string]any{"integer": json.Number("9007199254740993")}})
	frozen, err := freezeFileModel3DGLBConfig(&repository.Model3DGLBTaskSnapshot{TaskID: 17, TenantID: 7, Version: 3, Config: raw}, engine, candidate, executionID, "manager")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(frozen)
	config, err := decodeFileModel3DGLBConversion(encoded)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := newModel3DGLBSourceAuthorization(ctx, commonClient.NewSystemServiceClient(system.URL, ftpModelServiceTokens{}, system.Client()), execution, encoded, candidate)
	if err != nil {
		t.Fatal(err)
	}
	expectedScope = auth.scope.Clone()
	expectedExecution, expectedToken = executionID, token
	return ctx, Model3DGLBExecutionRequest{Task: &models.Model3DGLBTask{ID: 17, TenantID: 7}, ExecutionID: executionID, Config: config, SourceAuthorization: auth, inputOwner: &recordingModel3DStagingOwner{}}, engine, spy, calls
}

func ftpModelAuthorizedFixture(t *testing.T, format string, denied func(*ftpModelReadSpy) bool) (context.Context, Model3DGLBExecutionRequest, *commonModels.Engine, *ftpModelReadSpy, *atomic.Int32) {
	return fileModelAuthorizedFixture(t, "ftp", format, denied)
}

func TestNFSModelSourceUsesExactAuthorizedFiles(t *testing.T) {
	for _, format := range []string{"ifc", "3ds"} {
		for _, fault := range []string{"none", "revoked", "changed-declaration"} {
			t.Run(format+"/"+fault, func(t *testing.T) {
				ctx, request, engine, spy, _ := fileModelAuthorizedFixture(t, "nfs", format, func(*ftpModelReadSpy) bool { return fault == "revoked" })
				if fault == "changed-declaration" && format == "3ds" {
					spy.files["models/model.3ds"] = threeDSStagingInput("OTHER.PNG")
				}
				store := &stagingModelStore{}
				called := false
				err := stagingModelExecutor(store).withFileModel3DSource(ctx, request, engine, func(_ context.Context, source workflowaccess.Source, audit commonModels.JSONMap) error {
					called = true
					if source.Access.Method != workflowaccess.MethodObjectStore || audit["engine_type"] != "nfs" {
						t.Fatal("NFS bypassed exact staging")
					}
					return nil
				})
				rejected := fault == "revoked" || fault == "changed-declaration" && format == "3ds"
				if (err != nil) != rejected || called == rejected || len(store.objects) != 0 {
					t.Fatalf("err=%v called=%v residual=%d", err, called, len(store.objects))
				}
			})
		}
	}
}

func TestAuthorizedFTPModelSourceStagesOnlyFrozenFilesAndCleansThem(t *testing.T) {
	for _, format := range []string{"ifc", "3ds"} {
		t.Run(format, func(t *testing.T) {
			ctx, request, engine, spy, calls := ftpModelAuthorizedFixture(t, format, nil)
			store := &stagingModelStore{}
			consumed := false
			err := stagingModelExecutor(store).withFileModel3DSource(ctx, request, engine, func(_ context.Context, source workflowaccess.Source, audit commonModels.JSONMap) error {
				consumed = true
				if source.Access.Method != workflowaccess.MethodObjectStore || source.Format != format || len(store.objects) != len(spy.files) {
					t.Fatal("incomplete or wrong staging plan")
				}
				for name, data := range spy.files {
					found := false
					for key, value := range store.objects {
						if strings.HasSuffix(key, "/"+strings.TrimPrefix(name, "models/")) && value == string(data) {
							found = true
						}
					}
					if !found {
						t.Fatal("staged bytes differ from the authorized stream")
					}
				}
				raw, _ := json.Marshal(audit)
				if strings.Contains(string(raw), "connection") {
					t.Fatal("FTP connection leaked into audit")
				}
				return nil
			})
			if err != nil || !consumed || len(store.objects) != 0 || int(calls.Load()) < len(spy.files)*2 || spy.closes != len(spy.files) {
				t.Fatalf("err=%v consumed=%v residual=%d checks=%d closes=%d", err, consumed, len(store.objects), calls.Load(), spy.closes)
			}
			expected := []string{"models/model." + format}
			if format == "3ds" {
				expected = append(expected, "models/TEST.PNG")
			}
			if !reflect.DeepEqual(spy.opens, expected) || !reflect.DeepEqual(spy.facts, expected) {
				t.Fatalf("unexpected exact inputs: facts=%v opens=%v", spy.facts, spy.opens)
			}
		})
	}
}

func TestFTPModelSourceRevocationAndChangedReferencesStopBeforeStaging(t *testing.T) {
	for _, fault := range []string{"revoked-before-facts", "revoked-before-open", "revoked-after-open", "changed-reference", "empty-required-texture", "wrong-lease", "expired-local", "wrong-fingerprint", "changed-output", "changed-option"} {
		t.Run(fault, func(t *testing.T) {
			deny := func(s *ftpModelReadSpy) bool {
				facts, opens := s.readCounts()
				return fault == "revoked-before-facts" || (fault == "revoked-before-open" && facts > 0) || (fault == "revoked-after-open" && opens > 0)
			}
			ctx, request, engine, spy, _ := ftpModelAuthorizedFixture(t, "3ds", deny)
			store := &stagingModelStore{}
			consumed := false
			switch fault {
			case "changed-reference":
				spy.files["models/model.3ds"] = threeDSStagingInput("OTHER.PNG")
			case "empty-required-texture":
				spy.files["models/TEST.PNG"] = nil
			case "wrong-lease":
				lease, _ := commonExecution.LeaseFromContext(ctx)
				lease.Attempt++
				ctx = commonExecution.ContextWithLease(ctx, lease)
			case "expired-local":
				request.SourceAuthorization.expiresAt = time.Now().Add(-time.Second)
			case "wrong-fingerprint":
				request.Config.Source.ItemFingerprint = "wrong"
			case "changed-output":
				request.Config.Result.StorageRef = "different-target"
			case "changed-option":
				request.Config.Options["integer"] = json.Number("9007199254740992")
			}
			err := stagingModelExecutor(store).withFileModel3DSource(ctx, request, engine, func(context.Context, workflowaccess.Source, commonModels.JSONMap) error { consumed = true; return nil })
			maxFacts := 1
			if fault == "empty-required-texture" {
				maxFacts = 2
			}
			if err == nil || consumed || store.puts != 0 || len(spy.facts) > maxFacts || len(spy.opens) > 1 || spy.closes != len(spy.opens) {
				t.Fatalf("invalid source reached dependency read or staging: err=%v puts=%d facts=%v opens=%v closes=%d", err, store.puts, spy.facts, spy.opens, spy.closes)
			}
			if fault == "revoked-before-open" && len(spy.opens) != 0 {
				t.Fatal("source opened after revocation")
			}
			if fault == "revoked-before-facts" && len(spy.facts) != 0 {
				t.Fatal("source facts read after revocation")
			}
		})
	}
}

func TestAuthorizedFTPModelConversionFailureCleansAllPreparedInputs(t *testing.T) {
	ctx, request, engine, spy, _ := ftpModelAuthorizedFixture(t, "3ds", nil)
	store := &stagingModelStore{}
	conversionFailure := errors.New("conversion rejected the prepared model")
	invoked := false
	err := stagingModelExecutor(store).withFileModel3DSource(ctx, request, engine, func(context.Context, workflowaccess.Source, commonModels.JSONMap) error {
		invoked = true
		if len(store.objects) != 2 {
			t.Fatal("conversion saw an incomplete source")
		}
		return conversionFailure
	})
	if !errors.Is(err, conversionFailure) || !invoked || store.puts != 2 || len(store.objects) != 0 || spy.closes != 2 {
		t.Fatalf("conversion failure lost its cause or temporary inputs: err=%v invoked=%v puts=%d residual=%d closes=%d", err, invoked, store.puts, len(store.objects), spy.closes)
	}
}

func TestFTPModelBuildUsesFrozenNumbersCreateTargetAndCurrentPublicationAuthorization(t *testing.T) {
	for _, scenario := range []string{"success", "empty-runtime-response", "empty-object", "revoked-before-publish"} {
		t.Run(scenario, func(t *testing.T) {
			invoked := &atomic.Bool{}
			ctx, request, sourceEngine, _, _ := ftpModelAuthorizedFixture(t, "ifc", func(*ftpModelReadSpy) bool { return scenario == "revoked-before-publish" && invoked.Load() })
			bucket, object, err := rastercogref.ObjectLocation(request.Config.Result.StorageRef, "manager")
			if err != nil {
				t.Fatal(err)
			}
			sourceSystem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/system/engines/33" {
					t.Error("unexpected source lookup")
					w.WriteHeader(404)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(sourceEngine)
			}))
			t.Cleanup(sourceSystem.Close)
			payloads := make(chan map[string]any, 1)
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == "/api/operators" {
					writeModel3DOperatorList(w, testModel3DWorkflowEngineType, []string{"direct"})
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/operators/ifc_to_glb/invoke" {
					t.Error("unexpected conversion route")
					w.WriteHeader(404)
					return
				}
				var payload map[string]any
				decoder := json.NewDecoder(r.Body)
				decoder.UseNumber()
				if decoder.Decode(&payload) != nil {
					t.Error("invalid operator request")
					w.WriteHeader(400)
					return
				}
				payloads <- payload
				invoked.Store(true)
				if scenario == "empty-runtime-response" {
					io.WriteString(w, `{}`)
					return
				}
				facts := ftpModelPublicationFixture()
				values := facts.Result["result"].(map[string]any)
				values["glb_ref"], values["glb_uri"] = object, "s3://"+bucket+"/"+object
				publish := values["publish"].(map[string]any)
				publish["bucket"], publish["object_name"], publish["object_uri"] = bucket, object, values["glb_uri"]
				json.NewEncoder(w).Encode(map[string]any{"status": facts.Status, "execution_id": facts.ExecutionID, "result": values})
			}))
			t.Cleanup(runtime.Close)
			_, port, err := net.SplitHostPort(strings.TrimPrefix(runtime.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			store := &stagingModelStore{}
			if scenario != "empty-object" {
				store.statSize = 512
			}
			engine := commonModels.Engine{ID: 99, Name: "Model3D", EngineType: testModel3DWorkflowEngineType, ConnectionInfo: commonModels.ConnectionInfo{"protocol": "http", "host": "127.0.0.1", "port": port}, LifecycleState: "active", ConnectionStatus: "online", Capabilities: testRasterWorkflowCapabilities(t, testModel3DWorkflowEngineType)}
			executor := NewManagerModel3DGLBExecutor(newTestSystemClient(sourceSystem.URL), recordingWorkflowLister{engines: []commonModels.Engine{engine}}, store, "minio:9000", "infra-key", "infra-secret", false, "manager", 0)
			result, buildErr := executor.BuildModel3DGLB(ctx, request)
			if !invoked.Load() || len(store.objects) != 0 {
				t.Fatalf("conversion did not run or left temporary inputs: err=%v remaining=%d", buildErr, len(store.objects))
			}
			if scenario == "success" {
				if buildErr != nil || result == nil || result.StorageRef != request.Config.Result.StorageRef || result.SizeBytes != 512 {
					t.Fatalf("result=%+v err=%v", result, buildErr)
				}
			} else if buildErr == nil || result != nil {
				t.Fatalf("unproven or revoked result was published: result=%+v err=%v", result, buildErr)
			}
			select {
			case payload := <-payloads:
				params := payload["params"].(map[string]any)
				if params["options"].(map[string]any)["integer"] != json.Number("9007199254740993") {
					t.Fatal("frozen Runtime option was rounded before HTTP invocation")
				}
				target := params["access_plan"].(map[string]any)["target"].(map[string]any)
				access := target["access"].(map[string]any)
				if target["write_mode"] != "create" || access["bucket"] != bucket || access["object"] != object {
					t.Fatal("conversion did not create its independently frozen target")
				}
				raw, _ := json.Marshal(params)
				if bytes.Contains(raw, []byte("current-consumption-connection")) || bytes.Contains(raw, []byte("classification-must-not-be-used")) {
					t.Fatal("FTP credentials reached the Runtime")
				}
			default:
				t.Fatal("operator request not captured")
			}
		})
	}
}
