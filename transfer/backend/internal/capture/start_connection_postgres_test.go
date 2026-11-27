package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/testpg"
)

func TestIntegrationPostgresCaptureTargetConnectionAfterSourceChecks(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	ctx := t.Context()
	conn := testpg.ConnInfoFromEnv(t)
	db, err := openPostgreSQL(conn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := fmt.Sprintf("cdc_start_%d", time.Now().UnixNano())
	testpg.CreateSchema(t, ctx, db, schema)
	for _, table := range []string{"orders (id bigint PRIMARY KEY)", "no_key (id bigint NOT NULL)", "orders_cdc (id bigint PRIMARY KEY)"} {
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+schema+"."+table); err != nil {
			t.Fatal(err)
		}
	}
	caps, err := engineplugin.MarshalEngineCapabilities((&postgresql.PostgreSQLPlugin{}).Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	raw := commonmodels.JSONString(caps)
	const sourcePath = "/api/v1/system/engines/12"
	const targetDescriptorPath = "/api/v1/system/runtime/engine-descriptors/20"
	const targetPath = "/api/v1/system/engines/20"
	for i, tc := range []struct {
		name, source, target, wantError string
		requestTarget                   bool
		changeVersion, denyCatalog      bool
	}{
		{name: "source primary key missing", source: "no_key", target: "new_target", wantError: "requires a primary key"},
		{name: "target already exists", source: "orders", target: "orders_cdc", requestTarget: true, wantError: "must not already exist"},
		{name: "target catalog unavailable", source: "orders", target: "new_target", requestTarget: true, denyCatalog: true, wantError: "list database CDC target namespace"},
		{name: "target version changed", source: "orders", target: "new_target", requestTarget: true, changeVersion: true, wantError: "target engine changed before catalog check"},
		{name: "target absent", source: "orders", target: "new_target", requestTarget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []string
			version := int64(20 + i)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("expected authenticated System GET")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case sourcePath:
					_ = json.NewEncoder(w).Encode(commonmodels.Engine{ID: 12, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: commonmodels.ConnectionInfo(conn)})
				case targetDescriptorPath:
					_ = json.NewEncoder(w).Encode(commonmodels.EngineRuntimeDescriptor{ID: 20, Version: version, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online", Capabilities: &raw})
				case targetPath:
					target := commonmodels.Engine{ID: 20, Version: version, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: commonmodels.ConnectionInfo(maps.Clone(conn))}
					if tc.changeVersion {
						target.Version++
					}
					if tc.denyCatalog {
						target.ConnectionInfo["user"] = schema // No such role exists in the test database.
					}
					_ = json.NewEncoder(w).Encode(target)
				default:
					t.Errorf("unexpected System request %q", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			tokens := captureConnectionToken{}
			system := client.NewSystemServiceClient(server.URL, tokens, server.Client())
			resolver := NewDatabasePlanResolver(planner.NewSystemEngineResolver(client.NewSystemClient(server.URL, tokens)), func(ctx context.Context, tenant, id uint) (*commonmodels.EngineRuntimeDescriptor, error) {
				if tenant != 7 {
					t.Fatalf("descriptor tenant = %d, want 7", tenant)
				}
				return system.WithTenantID(tenant).GetEngineRuntimeDescriptor(ctx, id)
			})
			task := cleanupConnectionTask()
			task.Config["source"].(map[string]interface{})["locator"] = fmt.Sprintf("addp://engine/12/path/%s/%s?type=table", schema, tc.source)
			target := task.Config["target"].(map[string]interface{})
			target["parent_locator"] = fmt.Sprintf("addp://engine/20/path/%s?type=schema", schema)
			target["name"] = tc.target
			store, connect, topics, source := &fakeCaptureStore{}, &fakeConnectControl{}, &fakeTopicControl{}, &fakeSourceResources{}
			supervisor, err := NewSupervisor(store, resolver, connect, topics, source, SupervisorConfig{
				TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
				StatusPollInterval: time.Millisecond, MonitorInterval: time.Second,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			resource, err := supervisor.Start(ctx, task)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("Start error = %v, want %q", err, tc.wantError)
				}
				if store.resource != nil || topics.created || connect.putConfig != nil || source.ensureCalls != 0 {
					t.Fatal("capture provisioning began after failed preflight")
				}
			} else if err != nil || resource == nil || !topics.created || connect.putConfig == nil {
				t.Fatalf("Start failed to reach capture provisioning: resource=%v err=%v", resource, err)
			}
			want := []string{sourcePath, targetDescriptorPath}
			if tc.requestTarget {
				want = append(want, targetPath)
			}
			if !slices.Equal(requests, want) {
				t.Fatalf("System requests = %v, want %v", requests, want)
			}
		})
	}
}
