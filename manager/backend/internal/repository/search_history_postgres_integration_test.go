package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerSearchHistoryIsolation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	for _, withHistory := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%t", withHistory), func(t *testing.T) {
			ctx := t.Context()
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			const migration = "049_scope_search_history_to_tenant.sql"
			if err := tx.Exec(`CREATE SCHEMA IF NOT EXISTS manager;
				DROP TABLE IF EXISTS manager.search_histories;
				CREATE TABLE IF NOT EXISTS manager.search_schema_migrations(name text PRIMARY KEY, checksum text NOT NULL)`).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec("DELETE FROM manager.search_schema_migrations WHERE name = ?", migration).Error; err != nil {
				t.Fatal(err)
			}

			created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
			updated := created.Add(time.Hour)
			if withHistory {
				if err := tx.Exec(`CREATE TABLE manager.search_histories (
					id bigserial PRIMARY KEY, tenant_id bigint, user_id bigint NOT NULL,
					query varchar(512) NOT NULL, created_at timestamptz, updated_at timestamptz);
					CREATE UNIQUE INDEX idx_search_history_user_query ON manager.search_histories(user_id,query);
					CREATE INDEX idx_search_histories_tenant_id ON manager.search_histories(tenant_id)`).Error; err != nil {
					t.Fatal(err)
				}
				if err := tx.Exec(`INSERT INTO manager.search_histories(tenant_id,user_id,query,created_at,updated_at)
					VALUES (7,31,'shared-term',?,?), (NULL,31,'without-tenant',?,?), (7,0,'without-user',?,?), (0,31,'invalid-tenant',?,?)`,
					created, updated, created, updated, created, updated, created, updated).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := migrateSearchSchema(tx, migration); err != nil {
				t.Fatal(err)
			}

			repo := NewSearchHistoryRepository(tx)
			list := func(tenantID, userID uint) []models.SearchHistory {
				t.Helper()
				rows, err := repo.List(ctx, tenantID, userID, 100)
				if err != nil {
					t.Fatal(err)
				}
				return rows
			}
			record := func(tenantID, userID uint, term string) {
				t.Helper()
				if err := repo.Record(ctx, tenantID, userID, term, 50); err != nil {
					t.Fatal(err)
				}
			}
			if withHistory {
				rows := list(7, 31)
				if len(rows) != 1 || rows[0].ID != 1 || rows[0].Query != "shared-term" ||
					!rows[0].CreatedAt.Equal(created) || !rows[0].UpdatedAt.Equal(updated) {
					t.Fatalf("migration changed the owned row: %+v", rows)
				}
			}
			var invalid int64
			if err := tx.Model(&models.SearchHistory{}).Where("tenant_id IS NULL OR tenant_id <= 0 OR user_id <= 0").Count(&invalid).Error; err != nil || invalid != 0 {
				t.Fatalf("unowned rows = %d, error = %v", invalid, err)
			}

			record(7, 31, "shared-term")
			record(8, 31, "shared-term")
			record(7, 32, "shared-term")
			first, otherTenant, otherUser := list(7, 31)[0], list(8, 31)[0], list(7, 32)[0]
			record(7, 31, "shared-term")
			again := list(7, 31)
			if len(again) != 1 || again[0].ID != first.ID || !again[0].CreatedAt.Equal(first.CreatedAt) ||
				!again[0].UpdatedAt.After(first.UpdatedAt) {
				t.Fatalf("repeat search did not update the existing item: %+v", again)
			}
			if err := migrateSearchSchema(tx, migration); err != nil {
				t.Fatal(err)
			}
			if got := list(7, 31); len(got) != 1 || got[0].ID != first.ID || !got[0].UpdatedAt.Equal(again[0].UpdatedAt) {
				t.Fatalf("restarting migration changed existing history: %+v", got)
			}
			for _, id := range []uint{otherTenant.ID, otherUser.ID, first.ID} {
				if err := repo.DeleteByID(ctx, 7, 31, id); err != nil {
					t.Fatal(err)
				}
			}
			if got := list(7, 31); len(got) != 0 {
				t.Fatalf("own item survived deletion: %+v", got)
			}
			for i := range 51 {
				record(7, 31, fmt.Sprintf("term-%02d", i))
			}
			if got := list(7, 31); len(got) != 50 || got[0].Query != "term-50" || got[49].Query != "term-01" {
				t.Fatalf("retention did not remove only the oldest entry: %+v", got)
			}
			if err := repo.DeleteAll(ctx, 7, 31); err != nil {
				t.Fatal(err)
			}
			if got := list(7, 31); len(got) != 0 {
				t.Fatalf("own rows survived clearing: %+v", got)
			}
			for _, other := range []models.SearchHistory{otherTenant, otherUser} {
				got := list(other.TenantID, other.UserID)
				if len(got) != 1 || got[0].ID != other.ID || !got[0].UpdatedAt.Equal(other.UpdatedAt) {
					t.Fatalf("operation touched another scope: %+v", got)
				}
			}
		})
	}
}
