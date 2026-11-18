package migration

import (
	"database/sql"
	"encoding/json"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
)

func TestDevelopControlledFileInputsForwardMigrationAgainstPostgres(t *testing.T) {
	t.Run("format boundary", func(t *testing.T) {
		testAuthorizationFactsMigration(t, "000290_develop_controlled_file_inputs.up.sql", 290, 0, func(db *sql.DB) func() {
			return func() {
				object, err := plugin.ObjectItemPathFromRefPath(11, "bucket/input.geojson")
				if err != nil {
					t.Fatal(err)
				}
				for _, scenario := range []struct {
					format string
					target plugin.EngineCatalogPath
					valid  bool
				}{
					{"geojson", object, true}, {"geopackage", plugin.FileItemPath(11, "input.gpkg"), true},
					{"geopackage", object, false}, {"geojson", plugin.FileItemPath(11, "input.geojson"), false},
					{"", object, false}, {"tiff", object, false},
				} {
					scope := map[string]any{"schema_version": "develop.file-bound-scope/v1", "intent_digest": strings.Repeat("a", 64), "expected_attempt": 1, "timeout_seconds": 45, "sources": []any{map[string]any{"target": scenario.target, "format": scenario.format, "item_fingerprint": commonmodels.GenerateItemFingerprint(11, scenario.target.StringPath()), "boundary_hash": strings.Repeat("b", 64), "engine_version": "1"}}}
					raw, err := json.Marshal(scope)
					if err != nil {
						t.Fatal(err)
					}
					var valid bool
					if err := db.QueryRow("SELECT system.valid_develop_file_scope($1::jsonb)", string(raw)).Scan(&valid); err != nil || valid != scenario.valid {
						t.Fatal("controlled scope constraint", scenario.format, valid, err)
					}
				}
			}
		})
	})

	t.Run("scheduled parent forward upgrade", func(t *testing.T) {
		testAuthorizationFactsMigration(t, "000302_develop_scheduled_file_inputs.up.sql", 302, 0, func(db *sql.DB) func() {
			user, tenant := seedInitializedMigrationTenant(t, db, "scheduled-file-migration", "Scheduled File Migration")
			gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := execution.EnsureStore(gdb); err != nil {
				t.Fatal(err)
			}
			var membership, version int64
			if err := db.QueryRow("SELECT m.id,p.authorization_version FROM system.principals p JOIN system.tenant_memberships m ON p.id=m.principal_id WHERE p.id=$1 AND m.tenant_id=$2", user, tenant).Scan(&membership, &version); err != nil {
				t.Fatal(err)
			}
			var originalID int64
			attempt := func(parentTrigger, childTrigger string, root, saving, expired bool) (int64, error) {
				stamp := time.Now().UTC()
				expiry := stamp.Add(time.Minute)
				if expired {
					expiry = stamp.Add(-time.Second)
				}
				by := int(user)
				owner, token := "migration-parent", uuid.NewString()
				parent := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: "orchestrator", TaskType: "orchestration", Source: "orchestrator", ExecutionBoundary: "bounded", TriggerType: parentTrigger, Status: "running", Attempt: 1, MaxAttempts: 1, StartedAt: &stamp, LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &expiry, ActorPrincipalID: &user, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, TriggeredBy: &by, ExecutionConfig: commonmodels.JSONMap{}}
				if err := gdb.Create(parent).Error; err != nil {
					t.Fatal(err)
				}
				child := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: "develop", TaskType: "workflow", Source: "orchestrator", ParentExecutionID: &parent.ExecutionID, ExecutionBoundary: "bounded", TriggerType: childTrigger, Status: "pending", MaxAttempts: 1, ActorPrincipalID: &user, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, TriggeredBy: &by, ExecutionConfig: commonmodels.JSONMap{}}
				if root {
					child.Source = "develop"
					child.ParentExecutionID = nil
				}
				if err := gdb.Create(child).Error; err != nil {
					t.Fatal(err)
				}
				target := plugin.FileItemPath(11, "input.gpkg")
				scope := map[string]any{"schema_version": "develop.file-bound-scope/v1", "intent_digest": strings.Repeat("a", 64), "expected_attempt": 1, "timeout_seconds": 45, "sources": []any{map[string]any{"target": target, "format": "geopackage", "item_fingerprint": commonmodels.GenerateItemFingerprint(11, target.StringPath()), "boundary_hash": strings.Repeat("b", 64), "engine_version": "1"}}}
				if saving {
					scope["target"] = map[string]any{"target": plugin.FileItemPath(11, "output.gpkg"), "mode": "fail", "engine_version": "1"}
				}
				raw, err := json.Marshal(scope)
				if err != nil {
					t.Fatal(err)
				}
				var id int64
				err = db.QueryRow(`INSERT INTO system.execution_authorizations(actor_principal_id,tenant_id,tenant_membership_id,issued_authorization_version,source_type,execution_id,audience,expires_at,created_at,develop_file_scope) VALUES ($1,$2,$3,$4,'user',$5,'develop',clock_timestamp()+interval '5 minutes',clock_timestamp(),$6::jsonb) RETURNING id`, user, tenant, membership, version, child.ExecutionID, string(raw)).Scan(&id)
				return id, err
			}
			originalID, err = attempt("manual", "manual", false, false, false)
			if err != nil {
				t.Fatal("original manual parent", err)
			}
			if _, err := attempt("scheduled", "scheduled", false, false, false); err == nil {
				t.Fatal("pre-upgrade guard admitted scheduled child")
			}
			var original string
			if err := db.QueryRow("SELECT to_jsonb(e)::text FROM system.execution_authorizations e WHERE id=$1", originalID).Scan(&original); err != nil {
				t.Fatal(err)
			}
			return func() {
				var current string
				if err := db.QueryRow("SELECT to_jsonb(e)::text FROM system.execution_authorizations e WHERE id=$1", originalID).Scan(&current); err != nil || current != original {
					t.Fatal("upgrade changed original authorization", err)
				}
				for _, trigger := range []string{"manual", "scheduled"} {
					if _, err := attempt(trigger, trigger, false, false, false); err != nil {
						t.Fatal("valid upgraded parent", trigger, err)
					}
				}
				for _, tc := range []struct {
					name, parent, child   string
					root, saving, expired bool
				}{
					{"direct scheduled root", "scheduled", "scheduled", true, false, false},
					{"mismatched trigger", "manual", "scheduled", false, false, false},
					{"parent save", "scheduled", "scheduled", false, true, false},
					{"expired parent", "scheduled", "scheduled", false, false, true},
				} {
					t.Run(tc.name, func(t *testing.T) {
						if _, err := attempt(tc.parent, tc.child, tc.root, tc.saving, tc.expired); err == nil {
							t.Fatal("invalid parent chain received authorization")
						}
					})
				}
			}
		})
	})
}
