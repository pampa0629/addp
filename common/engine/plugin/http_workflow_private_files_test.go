package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const privateRuntimeID = "5cb603c1-7544-42ca-a998-6a196a7e31c4"
const privateOwnerID = "b8bc19af-664b-42a8-aeb2-a226a6512767"

func privateTestDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func privateTestManifest() WorkflowPrivateFileManifest {
	return WorkflowPrivateFileManifest{SchemaVersion: WorkflowPrivateFilesAPIV2, TenantID: 2, ExecutionID: privateOwnerID,
		Attempt: 1, AuthorizationID: 101, ScopeDigest: strings.Repeat("a", 64), Deadline: time.Now().Add(time.Minute),
		WorkflowJSON: `{"tasks":[{"id":"load","operator":"load","params":{},"depends_on":[]},{"id":"save","operator":"save","params":{"input_df":{"$ref":"load"},"mode":"fail"},"depends_on":["load"]}]}`,
		Inputs:       []WorkflowPrivateFile{{NodeID: "load", Format: "geopackage", Size: 5, SHA256: privateTestDigest([]byte("input"))}}, Outputs: []string{"save"}}
}

func privateTestStatus(manifest WorkflowPrivateFileManifest, output []byte) WorkflowPrivateFileStatus {
	pending := false
	return WorkflowPrivateFileStatus{RuntimeExecutionID: privateRuntimeID, ExecutionID: manifest.ExecutionID,
		Attempt: manifest.Attempt, ScopeDigest: manifest.ScopeDigest, Inputs: []string{"ready"},
		Execution: json.RawMessage(`{"status":"success"}`), CleanupPending: &pending,
		Outputs: []WorkflowPrivateFile{{NodeID: "save", Format: "geopackage", Size: int64(len(output)), SHA256: privateTestDigest(output)}}}
}

func privateTestClient(t *testing.T, server *httptest.Server, manifest WorkflowPrivateFileManifest, credential WorkflowRuntimeCredential) *WorkflowPrivateFilesClient {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	client, err := NewWorkflowPrivateFilesClient(ConnectionInfo{"host": u.Hostname(), "port": port, "protocol": u.Scheme, "password": "never-sent"}, privateRuntimeID, manifest, credential)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestWorkflowPrivateFilesCompleteProtocolUsesFreshCredentialAndOriginalCanonicalExecution(t *testing.T) {
	manifest := privateTestManifest()
	output := []byte("complete-encoded-output")
	status := privateTestStatus(manifest, output)
	var tokenCalls, calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer current-"+strconv.Itoa(int(count)) || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("not a fresh request-local service credential")
		}
		raw, _ := io.ReadAll(r.Body)
		if bytes.Contains(raw, []byte("current-")) || bytes.Contains(raw, []byte("never-sent")) {
			t.Error("credential entered protocol body")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "PUT /api/private-files/" + privateRuntimeID:
			var got WorkflowPrivateFileManifest
			if json.Unmarshal(raw, &got) != nil || got.Inputs[0].NodeID != "load" || got.Outputs[0] != "save" {
				t.Error("caller mutation changed frozen manifest")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(status)
		case "PUT /api/private-files/" + privateRuntimeID + "/inputs/0":
			if !bytes.Equal(raw, []byte("input")) || r.Header.Get("Content-Type") != "application/octet-stream" || r.ContentLength != -1 {
				t.Error("input was not transferred as a complete framed stream")
			}
			_, _ = w.Write([]byte(`{"runtime_execution_id":"` + privateRuntimeID + `","input_index":0,"state":"ready"}`))
		case "POST /api/workflow":
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			if len(fields) != 1 || fields["runtime"] == nil || bytes.Contains(raw, []byte("write")) || r.Header.Get("Accept-Language") != "en" {
				t.Error("private dispatch replaced frozen DAG or expanded business effects")
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"execution_id":"` + privateRuntimeID + `","status":"running"}`))
		case "GET /api/executions/" + privateRuntimeID:
			_, _ = w.Write([]byte(`{"execution_id":"` + privateRuntimeID + `","status":"success","result":{"preview":true}}`))
		case "GET /api/private-files/" + privateRuntimeID:
			_ = json.NewEncoder(w).Encode(status)
		case "GET /api/private-files/" + privateRuntimeID + "/outputs/0":
			w.Header().Set("Content-Type", "application/geopackage+sqlite3")
			_, _ = w.Write(output)
		case "DELETE /api/private-files/" + privateRuntimeID:
			_, _ = w.Write([]byte(`{"cleanup":"passed"}`))
		default:
			t.Errorf("unexpected protocol path: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := privateTestClient(t, server, manifest, func(context.Context) (string, error) {
		return "current-" + strconv.Itoa(int(tokenCalls.Add(1))), nil
	})
	manifest.Inputs[0].NodeID, manifest.Outputs[0] = "forged", "forged"
	if _, err := client.Create(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Upload(t.Context(), 0, strings.NewReader("input")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Execute(t.Context(), "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Execution(t.Context()); err != nil {
		t.Fatal(err)
	}
	observed, err := client.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var target bytes.Buffer
	if err := client.CopyOutput(t.Context(), 0, observed.Outputs[0], &target); err != nil || !bytes.Equal(target.Bytes(), output) {
		t.Fatalf("output integrity: %v", err)
	}
	if result, err := client.Cleanup(t.Context()); err != nil || result.Cleanup != "passed" {
		t.Fatalf("cleanup: %v", err)
	}
	if tokenCalls.Load() != 7 || calls.Load() != 7 {
		t.Fatal("implicit request or credential reuse")
	}
}

func TestWorkflowPrivateFilesRedirectAndFailureDoNotReplayMutation(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusServiceUnavailable, http.StatusConflict} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/unexpected-replay")
				w.WriteHeader(status)
			}))
			defer server.Close()
			client := privateTestClient(t, server, privateTestManifest(), func(context.Context) (string, error) { return "service", nil })
			if _, err := client.Create(t.Context()); err == nil || calls.Load() != 1 {
				t.Fatal("replayed or accepted failed creation")
			}
		})
	}
}

func TestWorkflowPrivateFilesOutputRequiresExactBytesEOFAndDigest(t *testing.T) {
	for _, kind := range []string{"short", "extra", "changed", "media"} {
		t.Run(kind, func(t *testing.T) {
			expected := []byte("complete")
			actual := append([]byte{}, expected...)
			if kind == "short" {
				actual = actual[:len(actual)-1]
			} else if kind == "extra" {
				actual = append(actual, 'x')
			} else if kind == "changed" {
				actual[0] ^= 1
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/geopackage+sqlite3")
				if kind == "media" {
					w.Header().Set("Content-Type", "application/json")
				}
				_, _ = w.Write(actual)
			}))
			defer server.Close()
			client := privateTestClient(t, server, privateTestManifest(), func(context.Context) (string, error) { return "service", nil })
			receipt := WorkflowPrivateFile{NodeID: "save", Format: "geopackage", Size: int64(len(expected)), SHA256: privateTestDigest(expected)}
			if err := client.CopyOutput(t.Context(), 0, receipt, io.Discard); err == nil || calls.Load() != 1 {
				t.Fatal("invalid output accepted or retried")
			}
		})
	}
}

func TestWorkflowPrivateFilesStatusRejectsWrongBindingAndIncompleteContract(t *testing.T) {
	for _, kind := range []string{"execution", "runtime", "attempt", "scope", "input", "input-incomplete", "output", "output-missing", "size", "digest", "cleanup", "unknown", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			manifest := privateTestManifest()
			status := privateTestStatus(manifest, []byte("output"))
			switch kind {
			case "execution":
				status.ExecutionID = privateRuntimeID
			case "runtime":
				status.RuntimeExecutionID = privateOwnerID
			case "attempt":
				status.Attempt++
			case "scope":
				status.ScopeDigest = strings.Repeat("b", 64)
			case "input":
				status.Inputs = nil
			case "input-incomplete":
				status.Inputs = []string{"missing"}
			case "output":
				status.Outputs[0].NodeID = "other"
			case "output-missing":
				status.Outputs = []WorkflowPrivateFile{}
			case "size":
				status.Outputs[0].Size = 0
			case "digest":
				status.Outputs[0].SHA256 = "invalid"
			case "cleanup":
				status.CleanupPending = nil
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "unknown" {
					_, _ = w.Write([]byte(`{"unknown":true}`))
					return
				}
				_ = json.NewEncoder(w).Encode(status)
				if kind == "trailing" {
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer server.Close()
			client := privateTestClient(t, server, manifest, func(context.Context) (string, error) { return "service", nil })
			if _, err := client.Status(t.Context()); err == nil {
				t.Fatal("invalid status accepted")
			}
		})
	}
}

func TestWorkflowPrivateFilesExpiredExecutionAllowsOnlyOriginalRecovery(t *testing.T) {
	manifest := privateTestManifest()
	manifest.Deadline = time.Now().Add(-time.Second)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"cleanup":"pending"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(privateTestStatus(manifest, []byte("output")))
	}))
	defer server.Close()
	client := privateTestClient(t, server, manifest, func(context.Context) (string, error) { return "service", nil })
	if _, err := client.Create(t.Context()); err == nil {
		t.Fatal("expired creation")
	}
	if err := client.Upload(t.Context(), 0, strings.NewReader("input")); err == nil {
		t.Fatal("expired upload")
	}
	if _, err := client.Execute(t.Context(), "en"); err == nil {
		t.Fatal("expired compute")
	}
	if err := client.CopyOutput(t.Context(), 0, WorkflowPrivateFile{NodeID: "save", Format: "geopackage", Size: 6, SHA256: privateTestDigest([]byte("output"))}, io.Discard); err == nil {
		t.Fatal("expired output retrieval")
	}
	if calls.Load() != 0 {
		t.Fatal("expired operation reached network")
	}
	if _, err := client.Status(t.Context()); err != nil {
		t.Fatal(err)
	}
	if result, err := client.Cleanup(t.Context()); err != nil || result.Cleanup != "pending" || calls.Load() != 2 {
		t.Fatalf("original cleanup misreported or business replayed: %v", err)
	}
}

func TestWorkflowPrivateFilesCapabilitiesRejectUnknownOrUnsupportedProtocol(t *testing.T) {
	for _, kind := range []string{"valid", "unknown", "unsupported", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			caps := NewWorkflowCapabilities("extension", WorkflowRuntimeAPIAddpV1)
			caps.Compute.Workflow.PrivateFilesAPI = WorkflowPrivateFilesAPIV2
			switch kind {
			case "unknown":
				caps.Compute.Workflow.PrivateFilesAPI = "other/v1"
			case "unsupported":
				caps.Compute.Workflow.Supported = false
			case "runtime":
				caps.Compute.Workflow.RuntimeAPI = "other/v1"
			}
			encoded, err := MarshalEngineCapabilities(caps)
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := ParseEngineCapabilities(encoded)
				if err != nil || parsed.Compute.Workflow.PrivateFilesAPI != WorkflowPrivateFilesAPIV2 {
					t.Fatalf("lost declared private protocol: %v", err)
				}
			} else if err == nil {
				t.Fatal("invalid private protocol declaration accepted")
			}
		})
	}
}

type privateDeclarationWithoutTransport struct {
	WorkflowRuntimeProvider
	caps EngineCapabilities
}

func (p *privateDeclarationWithoutTransport) Capabilities() EngineCapabilities { return p.caps }

func TestWorkflowPrivateFilesCapabilityRequiresActualProvider(t *testing.T) {
	caps := NewWorkflowCapabilities("extension", WorkflowRuntimeAPIAddpV1)
	provider := &privateDeclarationWithoutTransport{WorkflowRuntimeProvider: NewHTTPWorkflowRuntimeProvider("extension", "Extension"), caps: caps}
	if err := ValidatePluginCapabilities(provider); err != nil {
		t.Fatal(err)
	}
	provider.caps.Compute.Workflow.PrivateFilesAPI = WorkflowPrivateFilesAPIV2
	if err := ValidatePluginCapabilities(provider); err == nil || !strings.Contains(err.Error(), "private-file transport") {
		t.Fatalf("declaration without implementation accepted: %v", err)
	}
}

func TestWorkflowProfessionalTableBoundToOriginalExecutionAndCompleteRows(t *testing.T) {
	for _, scenario := range []string{"success", "unicode", "unavailable", "wrong-owner", "wrong-runtime", "wrong-attempt", "wrong-scope", "row-count", "duplicate-column", "unknown-field", "oversized", "trailing", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			manifest := privateTestManifest()
			manifest.Outputs = []string{}
			response := map[string]interface{}{"runtime_execution_id": privateRuntimeID, "execution_id": manifest.ExecutionID, "attempt": manifest.Attempt, "scope_digest": manifest.ScopeDigest, "available": true, "result": map[string]interface{}{"schema_version": "addp.workflow.table-result/v1", "columns": []string{"instance_id", "area"}, "rows": []map[string]interface{}{{"instance_id": 1, "area": 42.375}}, "row_count": 1}}
			table := response["result"].(map[string]interface{})
			switch scenario {
			case "unicode":
				table["rows"] = []map[string]interface{}{{"instance_id": 1, "area": strings.Repeat("建筑物", 4500)}}
			case "unavailable":
				response["available"] = false
				response["result"] = nil
			case "wrong-owner":
				response["execution_id"] = privateRuntimeID
			case "wrong-runtime":
				response["runtime_execution_id"] = privateOwnerID
			case "wrong-attempt":
				response["attempt"] = 2
			case "wrong-scope":
				response["scope_digest"] = strings.Repeat("b", 64)
			case "row-count":
				table["row_count"] = 2
			case "duplicate-column":
				table["columns"] = []string{"area", "area"}
			case "unknown-field":
				response["private_path"] = "must-not-be-returned"
			case "oversized":
				table["rows"] = []map[string]interface{}{{"instance_id": 1, "area": strings.Repeat("x", 70<<10)}}
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/private-files/"+privateRuntimeID+"/result" || r.Header.Get("Authorization") != "Bearer current" {
					t.Error("wrong private exchange")
				}
				if scenario == "denied" {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(response)
				if scenario == "trailing" {
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer server.Close()
			c := privateTestClient(t, server, manifest, func(context.Context) (string, error) { return "current", nil })
			value, err := c.ProfessionalResult(t.Context())
			switch scenario {
			case "success":
				if err != nil || value == nil || value.Rows[0]["area"].(json.Number).String() != "42.375" {
					t.Fatal(value, err)
				}
			case "unicode":
				if err != nil || value == nil || value.Rows[0]["area"] != strings.Repeat("建筑物", 4500) {
					t.Fatal("valid UTF-8 table rejected", err)
				}
			case "unavailable":
				if err != nil || value != nil {
					t.Fatal(value, err)
				}
			default:
				if err == nil || value != nil {
					t.Fatal("changed binding or table accepted", value, err)
				}
			}
			if calls != 1 {
				t.Fatal("private exchange replayed", calls)
			}
		})
	}
}
