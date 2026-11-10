package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/repository"
)

// Actual claimed Owner SQL, private HTTP client and local staging. Authority,
// NFS bytes and native computation remain explicit doubles, not business T4.
func TestPrivateFileWorkerDispatchRecoveryAndCapacity(t *testing.T) {
	for _, scenario := range []string{"success", "native-failure", "parameter-failure", "create-unknown", "execute-unknown", "cleanup-running", "cleanup-directory", "cleanup-unknown", "late-success"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, item, lease, _, issued, base := privateFileWorkerFixture(t, false)
			queue := repository.NewExecutionRepository(db)
			executor := NewDevExecutor(nil, execution.NewTaskExecutionRepository(db), nil, nil, nil, NewSQLEngineService(&config.Config{}, nil, nil), nil, nil)
			s, err := NewExecutionService(executor, queue, repo, "fixture-instance")
			if err != nil {
				t.Fatal(err)
			}
			f := s.privateFiles
			if scenario == "late-success" {
				base.ObservedAt = time.Now()
				base.ExpiresAt = time.Now().Add(300 * time.Millisecond)
			}
			var protectionBegun, protectionEnded int
			f.protectSource = func(_ context.Context, tenant uint, p plugin.EnginePlugin, path plugin.EngineCatalogPath) (func(), error) {
				if tenant != 3 || p.Type() != "nfs" || protectionBegun >= len(issued.Scope.Sources) ||
					!reflect.DeepEqual(path, issued.Scope.Sources[protectionBegun].Target) {
					t.Fatal("wrong protected source", tenant, path)
				}
				protectionBegun++
				return func() { protectionEnded++ }, nil
			}
			f.authority = func(uint) fileSourceAuthority {
				if protectionBegun != len(issued.Scope.Sources) || protectionEnded != 0 {
					t.Fatal("source I/O began outside the protection gate")
				}
				return fileInputsAuthority(base)
			}
			f.provider = fileInputsProvider(fileInputsData(t))
			f.credential = func(context.Context, uint) (string, error) { return "addp_at_runtime_current", nil }
			var creates, uploads, dispatches, deletes atomic.Int32
			var cleanupMode atomic.Int32
			switch scenario {
			case "cleanup-running":
				cleanupMode.Store(1)
			case "cleanup-directory":
				cleanupMode.Store(2)
			case "cleanup-unknown":
				cleanupMode.Store(3)
			}
			var runtimeID atomic.Value
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer addp_at_runtime_current" {
					t.Error("missing current tenant credential")
				}
				record, readErr := repo.ReadPrivateRuntime(r.Context(), 3, item.ExecutionID)
				if readErr != nil {
					t.Error("remote operation before durable original Runtime record", readErr)
					w.WriteHeader(500)
					return
				}
				id := record.RuntimeExecutionID
				switch {
				case r.Method == "PUT" && r.URL.Path == "/api/private-files/"+id:
					creates.Add(1)
					runtimeID.Store(id)
					var manifest plugin.WorkflowPrivateFileManifest
					_ = json.NewDecoder(r.Body).Decode(&manifest)
					if manifest.ExecutionID != item.ExecutionID || manifest.ScopeDigest != issued.ScopeDigest || !strings.Contains(manifest.WorkflowJSON, "9007199254740993") {
						t.Error("original binding or numeric input drifted")
					}
					if scenario == "create-unknown" {
						w.WriteHeader(503)
						return
					}
					states := make([]string, len(manifest.Inputs))
					for i := range states {
						states[i] = "missing"
					}
					no := false
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(plugin.WorkflowPrivateFileStatus{RuntimeExecutionID: id, ExecutionID: item.ExecutionID, Attempt: 1, ScopeDigest: manifest.ScopeDigest, Inputs: states, Outputs: []plugin.WorkflowPrivateFile{}, CleanupPending: &no, Execution: json.RawMessage(`{"status":"pending"}`)})
				case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/private-files/"+id+"/inputs/"):
					uploads.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					index, _ := strconv.Atoi(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
					_ = json.NewEncoder(w).Encode(map[string]any{"runtime_execution_id": id, "input_index": index, "state": "ready"})
				case r.Method == "POST" && r.URL.Path == "/api/workflow":
					dispatches.Add(1)
					raw, _ := io.ReadAll(r.Body)
					if strings.Contains(string(raw), "connection_info") {
						t.Error("generic connection leaked")
					}
					if scenario == "execute-unknown" {
						w.WriteHeader(503)
						return
					}
					w.WriteHeader(202)
					_ = json.NewEncoder(w).Encode(map[string]any{"execution_id": id, "status": "running"})
				case r.Method == "GET" && r.URL.Path == "/api/executions/"+id:
					if scenario == "late-success" {
						<-r.Context().Done()
						return
					}
					out := map[string]any{"execution_id": id, "status": "success", "result": map[string]any{"value": 7}}
					if scenario == "native-failure" {
						out["status"] = "failed"
						out["error_code"] = "EXECUTION_FAILED"
						out["error"] = "private path must not leak"
					}
					if scenario == "parameter-failure" {
						out["status"] = "failed"
						out["error_code"] = "INVALID_PARAMS"
						out["diagnostic"] = map[string]any{"schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": "operator_input_invalid", "operator": map[string]any{"node_index": 3, "operator_id": "buffer", "parameter": "input_gdf"}}
					}
					_ = json.NewEncoder(w).Encode(out)
				case r.Method == "DELETE" && r.URL.Path == "/api/private-files/"+id:
					deletes.Add(1)
					switch cleanupMode.Load() {
					case 1:
						w.WriteHeader(202)
						fmt.Fprint(w, `{"cleanup":"pending"}`)
					case 2:
						w.WriteHeader(202)
						fmt.Fprint(w, `{"cleanup":"pending","error_code":"PRIVATE_FILES_CLEANUP_FAILED"}`)
					case 3:
						w.WriteHeader(503)
					default:
						fmt.Fprint(w, `{"cleanup":"passed"}`)
					}
				default:
					t.Error("unexpected private consumer route", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			t.Cleanup(server.Close)
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			f.endpoint = func(context.Context, uint, uint) (plugin.ConnectionInfo, error) {
				return plugin.ConnectionInfo{"host": u.Hostname(), "port": port, "protocol": "http"}, nil
			}
			var claimed execution.TaskExecution
			_ = db.First(&claimed, "execution_id = ?", item.ExecutionID).Error
			if err := s.Execute(t.Context(), &claimed, *lease); err != nil {
				t.Fatal(err)
			}
			var done execution.TaskExecution
			if err := db.First(&done, "execution_id = ?", item.ExecutionID).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus := "success"
			if scenario == "late-success" || strings.Contains(scenario, "failure") || strings.HasSuffix(scenario, "unknown") && scenario != "cleanup-unknown" {
				wantStatus = "failed"
			}
			if scenario == "late-success" {
				payload, _ := json.Marshal(done.Metadata)
				if strings.Contains(string(payload), "final_result") {
					t.Fatal("expired input produced a business preview")
				}
			}
			if done.Status != wantStatus || creates.Load() != 1 || deletes.Load() != 1 {
				t.Fatal(done.Status, wantStatus, creates.Load(), deletes.Load())
			}
			wantDispatch := int32(1)
			if scenario == "create-unknown" {
				wantDispatch = 0
			}
			if dispatches.Load() != wantDispatch {
				t.Fatal("remote replay/missing dispatch", dispatches.Load())
			}
			private, _ := repo.ReadPrivateRuntime(t.Context(), 3, item.ExecutionID)
			if private.RuntimeExecutionID != runtimeID.Load() {
				t.Fatal("UUID replaced")
			}
			serialized, _ := json.Marshal(done)
			if strings.Contains(string(serialized), private.InputDirectory) || strings.Contains(string(serialized), private.RuntimeExecutionID) || strings.Contains(string(serialized), "private path must not leak") || done.Metadata["outputs"] != nil || done.Metadata["lineage_facts"] != nil {
				t.Fatal("private/readonly facts escaped", string(serialized))
			}
			if scenario == "parameter-failure" && done.ErrorDetails["code"] != "develop.workflow.invalid_params" {
				t.Fatal("lost original v3 parameter diagnostic", done.ErrorDetails)
			}
			wantUnstopped := 0
			if scenario == "cleanup-running" || scenario == "cleanup-unknown" {
				wantUnstopped = 1
			}
			if s.PrivateFilesUnstopped(map[string]struct{}{item.ExecutionID: {}}) != 0 {
				t.Fatal("same running batch counted twice")
			}
			if s.PrivateFilesUnstopped(nil) != wantUnstopped {
				t.Fatal("incorrect physical capacity", s.PrivateFilesUnstopped(nil))
			}
			if cleanupMode.Load() != 0 {
				if _, err := os.Stat(private.InputDirectory); err != nil {
					t.Fatal("pending directory removed", err)
				}
				// A persisted path from a different instance cannot send cleanup or drop its reservation.
				f.instanceID = "different-instance"
				before := deletes.Load()
				if s.RecoverPrivateFiles(t.Context()) == nil || deletes.Load() != before || s.PrivateFilesUnstopped(nil) != wantUnstopped {
					t.Fatal("unproven cleanup changed effects or prior physical observation", before, deletes.Load(), wantUnstopped, s.PrivateFilesUnstopped(nil))
				}
				f.instanceID = "fixture-instance"
				cleanupMode.Store(0)
				if err := s.RecoverPrivateFiles(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(private.InputDirectory); !os.IsNotExist(err) {
				t.Fatal("original local directory was not removed", err)
			}
			if s.PrivateFilesUnstopped(nil) != 0 || len(f.pending) != 0 {
				t.Fatal("stopped batch retained capacity")
			}
			before := creates.Load()
			_ = s.Execute(t.Context(), &claimed, *lease)
			if creates.Load() != before || dispatches.Load() != wantDispatch {
				t.Fatal("old execution was replayed")
			}
			if protectionBegun != len(issued.Scope.Sources) || protectionEnded != protectionBegun {
				t.Fatal("source protection lifetime not released", protectionBegun, protectionEnded)
			}
		})
	}
}

func TestPrivateFileWorkerClosesSaveAndUsesOriginalDeadline(t *testing.T) {
	for _, scenario := range []string{"save", "expired", "source-rejected", "protection-unavailable", "protected-second-source"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, item, lease, _, _, base := privateFileWorkerFixture(t, scenario == "save")
			executor := NewDevExecutor(nil, execution.NewTaskExecutionRepository(db), nil, nil, nil, NewSQLEngineService(&config.Config{}, nil, nil), nil, nil)
			s, err := NewExecutionService(executor, repository.NewExecutionRepository(db), repo, "fixture-instance")
			if err != nil {
				t.Fatal(err)
			}
			var protections, released int
			if scenario != "protection-unavailable" {
				s.privateFiles.protectSource = func(_ context.Context, _ uint, _ plugin.EnginePlugin, _ plugin.EngineCatalogPath) (func(), error) {
					protections++
					if scenario == "protected-second-source" && protections == 2 {
						return nil, errors.New("source is protected")
					}
					return func() { released++ }, nil
				}
			}
			var ioCalls atomic.Int32
			s.privateFiles.authority = func(uint) fileSourceAuthority {
				if scenario == "protected-second-source" || scenario == "protection-unavailable" {
					t.Error("protection rejection reached source authorization")
				}
				return fileInputsAuthority(base)
			}
			s.privateFiles.provider = &sourceWindowProvider{open: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
				ioCalls.Add(1)
				return nil, fmt.Errorf("must not open")
			}}
			s.privateFiles.credential = func(context.Context, uint) (string, error) { t.Error("unexpected runtime token"); return "", nil }
			s.privateFiles.endpoint = func(ctx context.Context, tenant, id uint) (plugin.ConnectionInfo, error) {
				if scenario == "expired" && ctx.Err() == nil {
					t.Error("Worker reset original deadline")
				}
				if scenario == "save" || scenario == "protected-second-source" || scenario == "protection-unavailable" {
					t.Error("rejected workflow reached Runtime resolution")
				}
				return plugin.ConnectionInfo{"host": "127.0.0.1", "port": 1, "protocol": "http"}, nil
			}
			var claimed execution.TaskExecution
			_ = db.First(&claimed, "execution_id = ?", item.ExecutionID).Error
			if scenario == "expired" {
				past := time.Now().Add(-601 * time.Second)
				claimed.StartedAt = &past
				_ = db.Model(&claimed).Update("started_at", past).Error
			}
			if scenario == "source-rejected" {
				s.privateFiles.authority = func(uint) fileSourceAuthority {
					return fileSourceAuthorityFunc(func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
						return nil, fmt.Errorf("read denied")
					})
				}
			}
			if err := s.Execute(t.Context(), &claimed, *lease); err != nil {
				t.Fatal(err)
			}
			if scenario == "protected-second-source" && (protections != 2 || released != 1) {
				t.Fatal("already-open protection read was not released", protections, released)
			}
			if ioCalls.Load() != 0 {
				t.Fatal("bad authorization reached source")
			}
			_ = s.RecoverPrivateFiles(t.Context())
		})
	}
}

func TestPrivateFileWorkerBlockedSourceRetainsOriginalBatchAndCapacity(t *testing.T) {
	db, repo, item, lease, _, _, base := privateFileWorkerFixture(t, false)
	executor := NewDevExecutor(nil, execution.NewTaskExecutionRepository(db), nil, nil, nil, NewSQLEngineService(&config.Config{}, nil, nil), nil, nil)
	s, err := NewExecutionService(executor, repository.NewExecutionRepository(db), repo, "fixture-instance")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(release) }) }
	defer stop()
	base.ObservedAt = time.Now()
	base.ExpiresAt = time.Now().Add(500 * time.Millisecond)
	f := s.privateFiles
	f.authority = func(uint) fileSourceAuthority { return fileInputsAuthority(base) }
	f.protectSource = func(context.Context, uint, plugin.EnginePlugin, plugin.EngineCatalogPath) (func(), error) {
		return func() {}, nil
	}
	f.provider = &sourceWindowProvider{open: func(_ context.Context, _ plugin.ConnectionInfo, p plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
		set, _ := plugin.NewQueryReadSet(p)
		return &sourceWindowHandle{Reader: bytes.NewReader(nil), set: set, close: func() error { <-release; return nil }}, nil
	}}
	f.endpoint = func(context.Context, uint, uint) (plugin.ConnectionInfo, error) {
		return plugin.ConnectionInfo{"host": "127.0.0.1", "port": 1, "protocol": "http"}, nil
	}
	f.credential = func(context.Context, uint) (string, error) {
		t.Error("incomplete source reached Runtime")
		return "", fmt.Errorf("forbidden")
	}
	var claimed execution.TaskExecution
	_ = db.First(&claimed, "execution_id = ?", item.ExecutionID).Error
	if err := s.Execute(t.Context(), &claimed, *lease); err != nil {
		t.Fatal(err)
	}
	if s.PrivateFilesUnstopped(nil) != 1 {
		t.Fatal("blocked source released its physical slot")
	}
	original, err := repo.ReadPrivateInputDirectory(t.Context(), 3, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadPrivateRuntime(t.Context(), 3, item.ExecutionID); err == nil {
		t.Fatal("blocked source invented a Runtime")
	}
	if err := s.RecoverPrivateFiles(t.Context()); err == nil || s.PrivateFilesUnstopped(nil) != 1 {
		t.Fatal("blocked source was falsely cleaned")
	}
	f.mu.Lock()
	batch := f.pending[item.ExecutionID].batch
	f.mu.Unlock()
	stop()
	waitSourceStopped(t, batch.sources[0])
	if err := s.RecoverPrivateFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(original.InputDirectory); !os.IsNotExist(err) || s.PrivateFilesUnstopped(nil) != 0 {
		t.Fatal("original stopped batch did not release", err)
	}
	var terminal execution.TaskExecution
	_ = db.First(&terminal, "execution_id = ?", item.ExecutionID).Error
	if terminal.Status != "failed" {
		t.Fatal("late close revived old execution", terminal.Status)
	}
}
