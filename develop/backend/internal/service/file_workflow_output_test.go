package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestPrivateWorkflowOutputDownload(t *testing.T) {
	for _, fault := range []string{"valid", "changed bytes", "truncated", "invalid geopackage"} {
		t.Run(fault, func(t *testing.T) {
			facts, issued, lease, base := fileInputsFixture(t, true)
			b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { b.runtime = nil; cleanupFileInputs(t, b) }()
			payload := fileInputsData(t)["sources/A.gpkg"]
			if fault == "invalid geopackage" {
				payload = []byte("not a GeoPackage")
			}
			sum := sha256.Sum256(payload)
			receipt := plugin.WorkflowPrivateFile{NodeID: b.manifest.Outputs[0], Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:])}
			downloads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer addp_at_runtime_current" {
					t.Error("wrong credential")
				}
				switch r.URL.Path {
				case "/api/private-files/" + fileInputRuntimeID:
					states := make([]string, len(b.manifest.Inputs))
					for i := range states {
						states[i] = "ready"
					}
					pending := false
					_ = json.NewEncoder(w).Encode(plugin.WorkflowPrivateFileStatus{RuntimeExecutionID: fileInputRuntimeID, ExecutionID: b.manifest.ExecutionID, Attempt: b.manifest.Attempt, ScopeDigest: b.manifest.ScopeDigest, Inputs: states, Outputs: []plugin.WorkflowPrivateFile{receipt}, CleanupPending: &pending, Execution: json.RawMessage(`{"status":"success"}`)})
				case "/api/private-files/" + fileInputRuntimeID + "/outputs/0":
					downloads++
					w.Header().Set("Content-Type", "application/geopackage+sqlite3")
					bytes := append([]byte(nil), payload...)
					if fault == "changed bytes" {
						bytes[0] ^= 255
					}
					if fault == "truncated" {
						bytes = bytes[:len(bytes)-1]
					}
					_, _ = w.Write(bytes)
				default:
					t.Error("unexpected request")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			uri, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(uri.Port())
			b.runtime, err = plugin.NewWorkflowPrivateFilesClient(plugin.ConnectionInfo{"host": uri.Hostname(), "port": port, "protocol": "http"}, fileInputRuntimeID, b.manifest, func(context.Context) (string, error) { return "addp_at_runtime_current", nil })
			if err != nil {
				t.Fatal(err)
			}
			file, got, err := b.downloadOutput(t.Context())
			if (err == nil) != (fault == "valid") || downloads != 1 {
				t.Fatal(fault, downloads, err)
			}
			if file != nil {
				defer file.Close()
				data, err := io.ReadAll(file)
				info, _ := file.Stat()
				if err != nil || string(data) != string(payload) || got != receipt || info.Mode().Perm() != os.FileMode(0600) {
					t.Fatal("download lost exact bytes or privacy")
				}
			}
			if again, _, err := b.downloadOutput(t.Context()); again != nil || err == nil || downloads != 1 {
				t.Fatal("download replayed")
			}
		})
	}
}
