package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/capture"
	"github.com/addp/transfer/internal/enginetest"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/protection"
	"github.com/addp/transfer/internal/testpg"
)

func TestSchemaChangeTargetConnectionOnlyAtTablePreparation(t *testing.T) {
	const source = "/api/v1/system/engines/12"
	const descriptor = "/api/v1/system/runtime/engine-descriptors/20"
	const target = "/api/v1/system/engines/20"
	const locator = "addp://engine/12/path/public/orders?type=table"
	db := newTransferTaskServiceTestDB(t)
	config, err := json.Marshal(validPostgreSQLCDCTaskConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"diff JSON", "capture_resource_id INTEGER", "generation INTEGER", "from_revision INTEGER", "to_revision INTEGER"} {
		if err := db.Exec("ALTER TABLE transfer.schema_change_requests ADD COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO transfer.transfer_tasks (id,apply_identity,tenant_id,name,task_type,config,status)
		VALUES (1,'cdc-test',7,'cdc','sync',?,'blocked')`, config).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO transfer.schema_change_requests (task_id,tenant_id,status,detected_at,diff,capture_resource_id,generation,from_revision,to_revision)
		VALUES (1,7,'pending',CURRENT_TIMESTAMP,'{"unexpected_fields":["added"]}',1,1,1,2)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE transfer.capture_resources AS SELECT 1 AS id,1 AS task_id,7 AS tenant_id,
		1 AS generation,1 AS schema_revision,'paused' AS status,'postgresql' AS source_type,'test' AS connector_name,'test' AS consumer_group,
		? AS source_identity,'business' AS source_database,'public' AS source_schema,'orders' AS source_table`, locator).Error; err != nil {
		t.Fatal(err)
	}

	caps, err := plugin.MarshalEngineCapabilities((&postgresql.PostgreSQLPlugin{}).Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	raw := commonmodels.JSONString(caps)
	field := models.SchemaChangeField{Source: "added", Target: "added", TargetType: "string", Nullable: true}
	for _, tc := range []struct {
		name                                                  string
		approve, sourceChanged, mappingChanged, targetChanged bool
		targetError                                           string
	}{
		{name: "view suggestions"},
		{name: "view after source changed", sourceChanged: true},
		{name: "approval rechecks source", approve: true, sourceChanged: true},
		{name: "approval rejects mapping", approve: true, mappingChanged: true},
		{name: "approval rejects changed target before DDL", approve: true, targetChanged: true, targetError: "target engine changed since inspection"},
		{name: "approval requests target for DDL", approve: true, targetError: "resolve target connection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads = append(reads, r.URL.Path)
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tenant-7" {
					t.Error("expected tenant 7 service GET")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case source:
					_ = json.NewEncoder(w).Encode(commonmodels.Engine{ID: 12, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: commonmodels.ConnectionInfo{"database": "business"}})
				case descriptor:
					_ = json.NewEncoder(w).Encode(commonmodels.EngineRuntimeDescriptor{ID: 20, Version: 9, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online", Capabilities: &raw})
				case target:
					if tc.targetChanged {
						_ = json.NewEncoder(w).Encode(commonmodels.Engine{ID: 20, Version: 10, EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online", Capabilities: &raw})
						return
					}
					http.Error(w, "target connection denied", http.StatusForbidden)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			svc := NewTaskService(db, nil, nil)
			tokens := recommendationTokenSource{}
			svc.SetEngineResolver(planner.NewSystemEngineResolver(client.NewSystemClient(server.URL, tokens)))
			svc.SetEngineDescriptorLookup(protection.EngineDescriptorLookup(client.NewSystemServiceClient(server.URL, tokens, server.Client())))
			svc.SetSchemaChangeInspector(func(_ context.Context, _ planner.DatabaseCDCTaskSpec, b planner.DatabaseCDCBindings, names []string) ([]models.SchemaChangeField, error) {
				reads = append(reads, "inspect")
				if b.Source.ConnInfo["database"] != "business" || b.Target.EngineVersion != 9 || b.Target.ConnInfo != nil || !slices.Equal(names, []string{"added"}) {
					t.Fatal("unexpected inspection inputs")
				}
				if tc.sourceChanged {
					return nil, errors.New("source primary key changed")
				}
				return []models.SchemaChangeField{field}, nil
			})
			ctx := transferTestUserOrigin()
			if tc.approve {
				f := field
				if tc.mappingChanged {
					f.Nullable = false
				}
				_, err := svc.ApproveSchemaChange(ctx, 1, 7, 9, models.ApproveSchemaChangeRequest{Fields: []models.SchemaChangeField{f}})
				if !errors.Is(err, ErrSchemaChangeApprovalConflict) || !strings.Contains(err.Error(), tc.targetError) {
					t.Fatalf("approval error = %v", err)
				}
			} else {
				view, err := svc.GetSchemaChange(ctx, 1, 7)
				if err != nil {
					t.Fatal(err)
				}
				if tc.sourceChanged {
					if view.Approvable || view.ApprovalBlockedReason != "source_validation_failed" {
						t.Fatalf("unexpected view %#v", view)
					}
				} else if !view.Approvable || !slices.Equal(view.SuggestedFields, []models.SchemaChangeField{field}) {
					t.Fatalf("unexpected suggestions %#v", view)
				}
			}
			want := []string{source, descriptor, "inspect"}
			if tc.targetError != "" {
				want = append(want, target)
			}
			if !slices.Equal(reads, want) {
				t.Fatalf("calls = %v, want %v", reads, want)
			}
		})
	}
}

func TestIntegrationPostgresSchemaChangeSourceReinspection(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	conn := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := fmt.Sprintf("cdc_field_review_%d", time.Now().UnixNano())
	testpg.CreateSchema(t, t.Context(), db, schema)
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE "+schema+".orders (id bigint PRIMARY KEY, added text)"); err != nil {
		t.Fatal(err)
	}
	config := validPostgreSQLCDCTaskConfig()
	config["source"].(map[string]interface{})["locator"] = "addp://engine/12/path/" + schema + "/orders?type=table"
	caps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	engines := planner.StaticEngineResolver{12: {Type: "postgresql", ConnInfo: conn}, 20: {Type: "postgresql", Capabilities: &caps}}
	svc := TaskService{engineResolver: engines, engineDescriptor: enginetest.Descriptors(engines), schemaInspector: capture.InspectAdditiveFields}
	task := models.TransferTask{Config: config, TenantID: 7}
	fields, bindings, err := svc.inspectSchemaChangeFields(t.Context(), &task, []string{"added"})
	if err != nil || !slices.Equal(fields, []models.SchemaChangeField{{Source: "added", Target: "added", TargetType: "string", Nullable: true}}) || bindings.Target.ConnInfo != nil {
		t.Fatalf("suggestions=%v err=%v", fields, err)
	}
	if _, err = db.ExecContext(t.Context(), "ALTER TABLE "+schema+".orders DROP CONSTRAINT orders_pkey"); err != nil {
		t.Fatal(err)
	}
	fields, _, err = svc.inspectSchemaChangeFields(t.Context(), &task, []string{"added"})
	if err == nil || !strings.Contains(err.Error(), "requires a primary key") || len(fields) != 0 {
		t.Fatalf("inspection after dropping %s primary key: fields=%v err=%v", schema, fields, err)
	}
}
