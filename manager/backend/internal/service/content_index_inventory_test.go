package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// Exercise the exact native Hosted handler on owned loopback fixtures, without
// invoking its deployment CLI or pretending the local host is a Hosted runner.
func nativeContentIndexProxy(t *testing.T, upstream string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(file), "../../../../scripts/test/native-effect-proxy.py")
	const program = `import importlib.util,sys
from http.server import ThreadingHTTPServer
from urllib.parse import urlsplit
spec=importlib.util.spec_from_file_location('native_proxy',sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
target=urlsplit(sys.argv[2]);assert target.scheme=='http' and target.hostname in {'127.0.0.1','localhost'} and not target.username
server=ThreadingHTTPServer(('127.0.0.1',0),m.HTTPProxy)
server.upstream=(target.hostname,target.port);server.fault=m.Fault('manager-transport-fixture')
print(server.server_address[1],flush=True)
server.serve_forever()
`
	cmd := exec.Command("python3", "-u", "-c", program, script, upstream)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	select {
	case line := <-ready:
		port, err := strconv.Atoi(line)
		if err != nil || port <= 0 || port > 65535 {
			t.Fatalf("owned proxy failed to start: %s", stderr.String())
		}
		return "http://127.0.0.1:" + line
	case <-time.After(5 * time.Second):
		t.Fatal("owned proxy readiness timed out")
		return ""
	}
}

func TestContentIndexInventoryNativeProxyTransport(t *testing.T) {
	var calls atomic.Int32
	origin := make(chan string, 1)
	svc, _ := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			origin <- "http://" + r.Host
			t.Logf("direct SDK Transfer-Encoding=%v, ContentLength=%d", r.TransferEncoding, r.ContentLength)
		}
		var query struct {
			Filter string `json:"filter"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/indexes/content/documents/fetch" ||
			n > 1 && (r.ContentLength <= 0 || len(r.TransferEncoding) != 0) || json.NewDecoder(r.Body).Decode(&query) != nil || query.Filter != "tenant_id = 7" {
			t.Error("proxy did not preserve and bound the SDK inventory request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"offset":0,"limit":256,"total":0}`))
	}))
	if _, err := svc.ReadContentIndexInventory(t.Context(), 7); err != nil {
		t.Fatalf("direct real SDK inventory baseline: %v", err)
	}
	proxy := nativeContentIndexProxy(t, <-origin)
	svc.client = meilisearch.New(proxy, meilisearch.WithCustomClient(&http.Client{Timeout: 2 * time.Second}), meilisearch.DisableRetries())
	svc.endpointID = contentIndexEndpointID(proxy)
	var err error
	svc.epoch, err = svc.deliveries.Open(t.Context(), svc.endpointID, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.deliveries.ActivateIfSettled(t.Context(), svc.epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadContentIndexInventory(t.Context(), 7); err != nil {
		t.Fatalf("real SDK through native proxy inventory: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("read-only inventory upstream calls: %d", calls.Load())
	}
}

func TestContentIndexInventoryCompleteness(t *testing.T) {
	for _, scenario := range []string{"complete", "empty", "duplicate", "wrong_tenant", "fractional_engine", "total_changed", "short_empty_page", "rapid_completed_write", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			var db *gorm.DB
			calls := 0
			svc, database := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/indexes/content/documents/fetch" {
					t.Errorf("unexpected mutation/route: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				var query struct {
					Offset int
					Limit  int
					Fields []string
					Filter string
				}
				if json.NewDecoder(r.Body).Decode(&query) != nil || query.Filter != "tenant_id = 7" || query.Limit != 256 || len(query.Fields) != 4 {
					t.Error("unbounded or unscoped inventory request")
				}
				total := 258
				if scenario == "empty" {
					total = 0
				}
				rows := []map[string]any{}
				for i := query.Offset; i < total && i < query.Offset+query.Limit; i++ {
					id := fmt.Sprintf("doc-%d", i)
					tenant := 7
					var engine any = uint64(9007199254740993)
					if scenario == "duplicate" && i == 257 {
						id = "doc-0"
					}
					if scenario == "wrong_tenant" {
						tenant = 8
					}
					if scenario == "fractional_engine" {
						engine = 1.5
					}
					rows = append(rows, map[string]any{"id": id, "document_id": id, "tenant_id": tenant, "engine_id": engine})
				}
				if scenario == "total_changed" && query.Offset > 0 {
					total++
				}
				if scenario == "short_empty_page" && query.Offset > 0 {
					rows = nil
				}
				if scenario == "rapid_completed_write" && calls == 1 {
					uid := int64(1)
					err := db.Create(&models.ContentIndexDelivery{ID: "concurrent-operation", IndexName: "content", TenantID: 7, DocumentID: "another", DocumentIDs: []string{"another"}, Kind: "write", Status: "succeeded", EndpointID: "endpoint", TaskCorrelation: "concurrent-operation", TaskUID: &uid, TaskEnqueuedAt: time.Now().UTC().Format(time.RFC3339Nano)}).Error
					if err != nil {
						t.Error(err)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"results": rows, "offset": query.Offset, "limit": query.Limit, "total": total})
			}))
			db = database
			if scenario == "pending" {
				if err := db.Create(&models.ContentIndexDelivery{ID: "pending-operation", IndexName: "content", TenantID: 7, Kind: "write", Status: "unknown", DocumentIDs: []string{}}).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, err := svc.ReadContentIndexInventory(t.Context(), 7)
			if scenario == "complete" || scenario == "empty" {
				if err != nil || got == nil || len(got.Digest) != 64 {
					t.Fatalf("inventory=%+v err=%v", got, err)
				}
				if scenario == "complete" && (len(got.Documents) != 258 || got.Documents[0].EngineID != uint(9007199254740993) || calls != 2) {
					t.Fatal("truncated identities or numeric precision loss")
				}
				var mutations int64
				db.Model(&models.ContentIndexDelivery{}).Count(&mutations)
				if mutations != 0 {
					t.Fatal("scan wrote delivery facts")
				}
			} else if err == nil || got != nil {
				t.Fatal("unverified inventory returned")
			}
			if scenario == "pending" && calls != 0 {
				t.Fatal("read index with unresolved delivery")
			}
		})
	}
}
