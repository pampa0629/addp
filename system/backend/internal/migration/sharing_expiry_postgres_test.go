package migration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSharingExpiryForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000172_engine_access_fulfillment_expiry_mode.up.sql", 172,
		"engine_access_fulfillment_outcomes", func(db *sql.DB) func() {
			gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			repo := iam.NewRepository(gormDB)
			user := &iam.Principal{PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
			if err := repo.Transaction(ctx, func(tx *iam.Repository) error {
				if err := tx.CreatePrincipal(ctx, user); err != nil {
					return err
				}
				return tx.CreateUser(ctx, &iam.User{ID: user.ID, DisplayName: "Finite history migration fixture"})
			}); err != nil {
				t.Fatal(err)
			}
			tenant, err := iam.NewPlatformTenantService(repo, time.Now).Create(ctx, iam.CreateTenantInput{
				Code: "sharing_expiry_fixture", Name: "Expiry", InitialAdministratorPrincipalID: user.ID, ActorPrincipalID: user.ID})
			if err != nil {
				t.Fatal(err)
			}
			tenantID := uint(tenant.ID)
			engine := models.Engine{Name: "Expiry fixture", EngineType: "postgresql", TenantID: &tenantID,
				LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{}, IdentityKey: models.JSONString(`{"host":"expiry.invalid","database":"fixture"}`)}
			if err := gormDB.Table("system.engines").Create(&engine).Error; err != nil {
				t.Fatal(err)
			}
			id := uuid.NewString()
			now := time.Now().UTC().Truncate(time.Microsecond)
			expires, deadline := now.Add(time.Hour), now.Add(5*time.Minute)
			if _, err := db.Exec(`INSERT INTO system.engine_access_fulfillment_outcomes
				(request_id, tenant_id, engine_id, caller_principal_id, catalog_path, binding, grant_expires_at, outcome, recorded_at, deadline)
				SELECT $1, $2, $3, id, '{}'::jsonb, jsonb_build_object('expires_at', $4::text, 'fixture', 'unchanged'), $5, 'accepted', $6, $7
				FROM system.service_principals WHERE name = 'addp-catalog'`, id, tenant.ID, engine.ID, expires.Format(time.RFC3339Nano), expires, now, deadline); err != nil {
				t.Fatal(err)
			}
			return func() {
				var mode, bindingMode, fixture string
				var gotExpiry, gotRecorded, gotDeadline time.Time
				if err := db.QueryRow(`SELECT expiry_mode, binding->>'expiry_mode', binding->>'fixture', grant_expires_at, recorded_at, deadline
					FROM system.engine_access_fulfillment_outcomes WHERE request_id = $1`, id).Scan(&mode, &bindingMode, &fixture, &gotExpiry, &gotRecorded, &gotDeadline); err != nil ||
					mode != "at_time" || bindingMode != "at_time" || fixture != "unchanged" || !gotExpiry.Equal(expires) || !gotRecorded.Equal(now) || !gotDeadline.Equal(deadline) {
					t.Fatalf("history extended or changed: %s/%s/%s %v/%v/%v err=%v", mode, bindingMode, fixture, gotExpiry, gotRecorded, gotDeadline, err)
				}
				for _, statement := range []string{
					"UPDATE system.engine_access_fulfillment_outcomes SET expiry_mode = 'until_revoked', grant_expires_at = NULL WHERE request_id = $1",
					"DELETE FROM system.engine_access_fulfillment_outcomes WHERE request_id = $1",
				} {
					if _, err := db.Exec(statement, id); err == nil {
						t.Fatal("migration failed to restore immutable history protection")
					}
				}
			}
		})
}
