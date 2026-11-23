package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestManagerGRIBCOGForwardMigrationAgainstPostgres(t *testing.T) {
	const migrationName = "000311_manager_grib_cog_source_authorization.up.sql"
	testAuthorizationFactsMigration(t, migrationName, 311, 0, func(db *sql.DB) func() {
		user, tenant := seedInitializedMigrationTenant(t, db, "manager-grib-cog", "Manager GRIB COG")
		gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := execution.EnsureStore(gdb); err != nil {
			t.Fatal(err)
		}
		var membership, version int64
		if err := db.QueryRow(`SELECT m.id,p.authorization_version
			FROM system.principals p JOIN system.tenant_memberships m ON m.principal_id=p.id
			WHERE p.id=$1 AND m.tenant_id=$2`, user, tenant).Scan(&membership, &version); err != nil {
			t.Fatal(err)
		}
		engineIDs := map[string]uint{}
		for _, kind := range []string{"nfs", "minio", "ftp"} {
			var id uint
			if err := db.QueryRow(`INSERT INTO system.engines
				(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
				VALUES($1,$2,$3,'{"host":"fixture.invalid"}','{"host":"fixture.invalid"}','active',false)
				RETURNING id`, tenant, "GRIB migration "+kind, kind).Scan(&id); err != nil {
				t.Fatal(err)
			}
			engineIDs[kind] = id
		}
		nfs := plugin.FileItemPath(engineIDs["nfs"], "grib/forecast.GRIB2")
		minio := plugin.ObjectItemPath(engineIDs["minio"], "weather", "grib/forecast.grb2")
		newComposition := func(target plugin.EngineCatalogPath) *authorizationfacts.FileCompositionBoundary {
			t.Helper()
			boundary, err := authorizationfacts.NewFileCompositionBoundary("grib", []authorizationfacts.FileCompositionRef{
				{Target: target, Role: "container", Required: true, Primary: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			return boundary
		}
		attempt := func(taskType, configVersion string, frozen, requested *authorizationfacts.FileCompositionBoundary) error {
			t.Helper()
			taskID, taskVersion, engineVersion := "123", "7", "1"
			config := models.JSONMap{
				"config_version": execution.ManagerRasterCOGConfigVersion, "owner_task": map[string]string{"id": taskID, "version": taskVersion},
				"source_engine_version": engineVersion, "source_composition": frozen,
				"target": map[string]any{}, "cog": map[string]any{}, "result": map[string]any{},
				"raster": map[string]any{"message": "message-1", "source_format": "grib", "source_version": "sha256:3c3f4aabaff2eb520c3efb4039db9d42336d39f1ae445bee56ee044e21b5fbe4"},
			}
			configJSON, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			scope, err := execution.NewManagerFileReadScope(configJSON, taskID, taskVersion, engineVersion, frozen)
			if err != nil {
				t.Fatal(err)
			}
			config["config_version"] = configVersion
			scope.FileComposition = *requested
			scopeJSON, err := json.Marshal(scope)
			if err != nil {
				t.Fatal(err)
			}
			actor := int(user)
			job := &execution.TaskExecution{
				ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: "manager", Source: "manager",
				TaskType: taskType, SourceTaskID: &taskID, TriggerType: "manual", TriggeredBy: &actor,
				Status: "pending", ExecutionBoundary: "bounded", MaxAttempts: 3, ExecutionConfig: config,
				ActorPrincipalID: &user, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version,
			}
			if err := execution.NewTaskExecutionRepository(gdb).Create(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(`INSERT INTO system.execution_authorizations
				(actor_principal_id,tenant_id,tenant_membership_id,issued_authorization_version,
				source_type,execution_id,audience,expires_at,created_at,source_read_scope)
				VALUES ($1,$2,$3,$4,'user',$5,'manager',now()+interval '5 minutes',now(),$6::jsonb)`,
				user, tenant, membership, version, job.ExecutionID, string(scopeJSON))
			return err
		}
		cog := func(frozen, requested *authorizationfacts.FileCompositionBoundary) error {
			return attempt(execution.TaskTypeRasterCOGGeneration, execution.ManagerRasterCOGConfigVersion, frozen, requested)
		}
		deny := func(t *testing.T, err error) {
			t.Helper()
			var pg *pq.Error
			if !errors.As(err, &pg) || pg.Code != "23514" || pg.Message != "source scope requires the exact pending Manager execution" {
				t.Fatalf("expected pending Manager scope rejection, got %v", err)
			}
		}
		file, object := newComposition(nfs), newComposition(minio)
		deny(t, cog(file, file))
		return func() {
			for _, composition := range []*authorizationfacts.FileCompositionBoundary{file, object} {
				if err := cog(composition, composition); err != nil {
					t.Fatalf("exact GRIB COG scope rejected after migration: %v", err)
				}
			}
			ftp := newComposition(plugin.FileItemPath(engineIDs["ftp"], "grib/forecast.grib2"))
			wrongStorageTerm := newComposition(plugin.FileItemPath(engineIDs["minio"], "grib/forecast.grib2"))
			deny(t, cog(ftp, ftp))
			deny(t, cog(wrongStorageTerm, wrongStorageTerm))
			deny(t, cog(file, object))
			deny(t, attempt(execution.TaskTypeModel3DGLBGeneration, execution.ManagerModel3DConfigVersion, file, file))
			extra := file.Clone()
			extra.Refs = append(extra.Refs, authorizationfacts.FileCompositionRef{
				Target: plugin.FileItemPath(engineIDs["nfs"], "grib/forecast.GRIB2.idx"), Role: "index",
			})
			deny(t, cog(file, extra))
		}
	})
}
