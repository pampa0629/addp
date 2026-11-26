package migration

import (
	"database/sql"
	"encoding/json"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
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

	t.Run("ordered output scope after upgrade", func(t *testing.T) {
		testAuthorizationFactsMigration(t, "000317_develop_file_approval_target_index.up.sql", 317, 0, func(db *sql.DB) func() {
			gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := execution.EnsureStore(gdb); err != nil {
				t.Fatal(err)
			}
			// The IAM gate owns a disposable database. Create only this test's
			// projection; a real Develop table must never be replaced.
			var ownsSchema bool
			if err := db.QueryRow("SELECT to_regnamespace('develop') IS NULL").Scan(&ownsSchema); err != nil {
				t.Fatal(err)
			}
			if ownsSchema {
				if _, err := db.Exec("CREATE SCHEMA develop"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec("CREATE TABLE develop.file_executions (execution_id text PRIMARY KEY,tenant_id bigint,intent_json jsonb,publication_json jsonb)"); err != nil {
				t.Fatal(err)
			}
			dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
			t.Cleanup(func() {
				clean, err := sql.Open("postgres", dsn)
				if err != nil {
					t.Error(err)
					return
				}
				defer clean.Close()
				if _, err := clean.Exec("DROP TABLE develop.file_executions"); err != nil {
					t.Error(err)
				}
				if ownsSchema {
					if _, err := clean.Exec("DROP SCHEMA develop"); err != nil {
						t.Error(err)
					}
				}
			})
			id := uuid.NewString()
			user, tenant := seedInitializedMigrationTenant(t, db, "old-file-cutover", "Old file cutover")
			var membership, authVersion int64
			if err := db.QueryRow("SELECT m.id,p.authorization_version FROM system.principals p JOIN system.tenant_memberships m ON m.principal_id=p.id WHERE p.id=$1 AND m.tenant_id=$2", user, tenant).Scan(&membership, &authVersion); err != nil {
				t.Fatal(err)
			}
			by := int(user)
			item := execution.TaskExecution{ExecutionID: id, TenantID: int(tenant), Module: "develop", TaskType: "workflow", Source: "develop", ExecutionBoundary: "bounded", TriggerType: "manual", Status: "pending", MaxAttempts: 1, ActorPrincipalID: &user, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &authVersion, TriggeredBy: &by, ExecutionConfig: commonmodels.JSONMap{}}
			if err := gdb.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			src := plugin.FileItemPath(11, "input.gpkg")
			oldScope, err := json.Marshal(map[string]any{"schema_version": "develop.file-bound-scope/v1", "intent_digest": strings.Repeat("a", 64), "expected_attempt": 1, "timeout_seconds": 45, "sources": []any{map[string]any{"target": src, "format": "geopackage", "item_fingerprint": commonmodels.GenerateItemFingerprint(11, src.StringPath()), "boundary_hash": strings.Repeat("b", 64), "engine_version": "1"}}, "target": map[string]any{"target": plugin.FileItemPath(11, "output.gpkg"), "mode": "fail", "engine_version": "1"}})
			if err != nil {
				t.Fatal(err)
			}
			var eaID int64
			if err := db.QueryRow(`INSERT INTO system.execution_authorizations(actor_principal_id,tenant_id,tenant_membership_id,issued_authorization_version,source_type,execution_id,audience,created_at,expires_at,develop_file_scope)
				VALUES ($1,$2,$3,$4,'user',$5,'develop',clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '1 minute',$6::jsonb) RETURNING id`, user, tenant, membership, authVersion, id, string(oldScope)).Scan(&eaID); err != nil {
				t.Fatal(err)
			}
			approvalID := uuid.NewString()
			digest := strings.Repeat("c", 64)
			if _, err := db.Exec(`INSERT INTO system.develop_file_create_approvals(id,authorization_id,scope_digest,approver_principal_id,approver_membership_id,approver_authorization_version,approved_at,expires_at)
				SELECT $1,id,$3,$4,$5,$6,created_at,expires_at FROM system.execution_authorizations WHERE id=$2`, approvalID, eaID, digest, user, membership, authVersion); err != nil {
				t.Fatal(err)
			}
			if err := gdb.Model(&item).Update("status", "failed").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO develop.file_executions VALUES($1,$2,'{"schema_version":"develop.file-intent/v1"}','{"dispatch_released_at":"2020-01-01T00:00:00Z"}')`, id, tenant); err != nil {
				t.Fatal(err)
			}
			_, through := migrationFilesBeforeAndThrough(t, "000317_develop_file_approval_target_index.up.sql")
			if err := (&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}).Run(t.Context()); err == nil || !strings.Contains(err.Error(), id) {
				t.Fatal("unresolved original did not block the installer", err)
			}
			var clean bool
			if err := db.QueryRow("SELECT version=316 AND NOT dirty FROM system.schema_migrations").Scan(&clean); err != nil || !clean {
				t.Fatal("rejected installation dirtied the old version", err)
			}
			settled := `{"dispatch_released_at":"2020-01-01T00:00:00Z","recovery_outcome":{"State":"published"},"initial_authorization":{"status":"issued"}}`
			if _, err := db.Exec("UPDATE develop.file_executions SET publication_json=$1::jsonb WHERE execution_id=$2", settled, id); err != nil {
				t.Fatal(err)
			}
			return func() {
				var preserved bool
				if err := db.QueryRow(`SELECT a.target_index=0 AND a.scope_digest=$2 AND ea.develop_file_scope=$3::jsonb FROM system.develop_file_create_approvals a JOIN system.execution_authorizations ea ON ea.id=a.authorization_id WHERE a.id=$1`, approvalID, digest, string(oldScope)).Scan(&preserved); err != nil || !preserved {
					t.Fatal("migration rewrote the original approval or scope", err)
				}
				var retained bool
				if err := db.QueryRow("SELECT publication_json=$1::jsonb FROM develop.file_executions WHERE execution_id=$2", settled, id).Scan(&retained); err != nil || !retained {
					t.Fatal("migration rewrote original operation", err)
				}
				source := plugin.FileItemPath(11, "sources/input.gpkg")
				scope := map[string]any{"schema_version": execution.DevelopBoundFileScopeSchema, "intent_digest": strings.Repeat("a", 64), "expected_attempt": 1, "timeout_seconds": 45, "sources": []any{map[string]any{"target": source, "format": "geopackage", "item_fingerprint": commonmodels.GenerateItemFingerprint(11, source.StringPath()), "boundary_hash": strings.Repeat("b", 64), "engine_version": "1"}}}
				target := func(name string) map[string]any {
					return map[string]any{"target": plugin.FileItemPath(11, name), "mode": "fail", "engine_version": "2"}
				}
				first, second := target("out/a.gpkg"), target("out/b.gpkg")
				for _, tc := range []struct {
					name    string
					targets []any
					valid   bool
				}{
					{"two exact files", []any{first, second}, true}, {"duplicate file", []any{first, first}, false}, {"empty saved list", []any{}, false},
				} {
					t.Run(tc.name, func(t *testing.T) {
						scope["targets"] = tc.targets
						raw, err := json.Marshal(scope)
						if err != nil {
							t.Fatal(err)
						}
						var valid bool
						if err := db.QueryRow("SELECT system.valid_develop_file_scope($1::jsonb)", string(raw)).Scan(&valid); err != nil || valid != tc.valid {
							t.Fatal("unexpected v2 scope acceptance", valid, err)
						}
					})
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
