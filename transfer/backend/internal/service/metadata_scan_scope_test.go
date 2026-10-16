package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

// Exercise both production submission paths through the real Meta HTTP client.
func TestNativeTargetMetadataScanSubmitsOnlyTableLeaf(t *testing.T) {
	for _, entry := range []string{"bounded", "schema_change"} {
		for _, tc := range []struct{ name, parent, table, target string }{
			{"schema", "addp://engine/9/path/public?type=schema", "roads", "addp://engine/9/path/public/roads?type=table"},
			{"parent_node_id", "addp://engine/9/path/public?type=schema&node_id=123", "roads", "addp://engine/9/path/public/roads?type=table"},
			{"database", "addp://engine/9/path/business?type=database", "orders", "addp://engine/9/path/business/orders?type=table"},
			{"dots", "addp://engine/9/path/sales.v2?type=schema", "orders.2026", "addp://engine/9/path/sales.v2/orders.2026?type=table"},
			{"missing_name", "addp://engine/9/path/public?type=schema", "", ""},
			{"no_engine", "addp://engine/0/path/public?type=schema", "roads", ""},
			{"missing_parent", "", "roads", ""},
			{"content_parent", "addp://engine/9/path/exports?type=directory", "roads", ""},
		} {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				var body struct {
					EngineID     uint                            `json:"engine_id"`
					Targets      []string                        `json:"targets"`
					CatalogPaths []string                        `json:"catalog_paths"`
					RefGroups    []commonClient.MetaScanRefGroup `json:"ref_groups"`
					ScanDepth    string                          `json:"scan_depth"`
					Force        bool                            `json:"force"`
					Source       string                          `json:"source"`
				}
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != "/api/v1/meta/scan/run/manual" {
						t.Errorf("unexpected Meta request: %s %s", r.Method, r.URL.Path)
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"execution_id":"meta-leaf-run","module":"meta","task_type":"scan","status":"pending"}`))
				}))
				defer server.Close()
				client := newExecutionTestMetaClient(server.URL)
				logger := slog.New(slog.NewTextHandler(io.Discard, nil))
				endpoint := planner.EndpointSpec{Representation: "native", ParentLocator: tc.parent, Name: tc.table}
				if entry == "bounded" {
					svc := &ExecutionEngineService{metaClient: client, logger: logger}
					svc.triggerMetadataScan(&models.TransferTask{TenantID: 7}, 0,
						planner.TableExportTaskSpec{Target: endpoint}, executor.TableTargetPlan{Kind: executor.TableEndpointNative}, nil)
				} else {
					svc := &TaskService{metaClient: client, logger: logger}
					svc.runClaimedSchemaChangeMetaScan(context.Background(), 7, &models.SchemaChangeRequest{}, planner.DatabaseCDCTaskSpec{Target: endpoint})
				}
				if tc.target == "" {
					if calls != 0 {
						t.Fatalf("invalid target submitted %d scans: %#v", calls, body)
					}
					return
				}
				if calls != 1 || body.EngineID != 9 || !reflect.DeepEqual(body.Targets, []string{tc.target}) {
					t.Fatalf("scan requests=%d payload=%#v", calls, body)
				}
				if len(body.CatalogPaths) != 0 || len(body.RefGroups) != 0 || body.ScanDepth != "deep" || !body.Force || body.Source != "transfer" {
					t.Fatalf("unexpected scan scope/options: %#v", body)
				}
			})
		}
	}
}
