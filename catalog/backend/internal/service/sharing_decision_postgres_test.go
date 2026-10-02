package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresSharingDecisionIsAtomicImmutableAndRetryable(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable Catalog PostgreSQL gate")
	}
	// This fixture reconstructs the old schema after reading the current table.
	// Unlike startup (migration before reads), it must not reuse a prepared
	// SELECT * result layout across DROP/ADD COLUMN on the same connection.
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
	connection := stdlib.OpenDB(*config)
	defer connection.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db = db.Begin()
	if db.Error != nil {
		t.Fatal(db.Error)
	}
	defer db.Rollback()
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	entry, input := seedSharingEntry(t, db)
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	auth := sharingAuth()
	result, created, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, auth)
	if err != nil || !created {
		t.Fatalf("create=%+v created=%v err=%v", result, created, err)
	}
	if _, created, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, auth); err != nil || created {
		t.Fatalf("microsecond retry created=%v err=%v", created, err)
	}
	t.Run("current basis is transaction-bound and does not revive history", func(t *testing.T) {
		if _, err := lockCurrentSharingDecisionBasis(context.Background(), db, 7, entry.ID, result.ID); err != nil {
			t.Fatal(err)
		}
		for _, fixture := range []struct {
			name, statement string
			argument        any
			want            error
		}{
			{"prose only", "UPDATE catalog.entries SET business_name = 'New business name', version = version + 1 WHERE id = ?", entry.ID, nil},
			{"original owner inactive", "UPDATE catalog.responsibilities SET status = 'needs_transfer' WHERE id = ?", result.ResponsibilityID, ErrSharingConfirmationForbidden},
			{"source changed", "UPDATE catalog.source_bindings SET source_version = '00000000000000000002' WHERE id = ?", result.SourceBindingID, ErrSharingTargetUnsupported},
		} {
			t.Run(fixture.name, func(t *testing.T) {
				rollback := errors.New("rollback current-basis fixture")
				err := db.Transaction(func(tx *gorm.DB) error {
					if err := tx.Exec(fixture.statement, fixture.argument).Error; err != nil {
						return err
					}
					basis, err := lockCurrentSharingDecisionBasis(context.Background(), tx, 7, entry.ID, result.ID)
					if !errors.Is(err, fixture.want) || (err != nil && basis != nil) {
						t.Fatalf("basis=%+v err=%v want=%v", basis, err, fixture.want)
					}
					if basis != nil && (basis.ID != result.ID || !basis.CreatedAt.Equal(result.CreatedAt) || basis.EntryVersion != result.EntryVersion) {
						t.Fatalf("basis changed decision: %+v", basis)
					}
					// History remains readable even when not a current basis.
					reader := NewEntryService(tx, nil, nil)
					if _, err := reader.GetSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, result.ID, auth); err != nil {
						t.Fatalf("historical decision lost: %v", err)
					}
					return rollback
				})
				if !errors.Is(err, rollback) {
					t.Fatal(err)
				}
			})
		}
	})
	t.Run("preparation binds persisted decision and protects basis", func(t *testing.T) {
		rollback := errors.New("rollback pending preparation fixture")
		err := db.Transaction(func(tx *gorm.DB) error {
			request := preparationFixture(t, result)
			first, created, err := prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, result.ID, request)
			if err != nil || !created || first.ResolvedAt != nil {
				t.Fatalf("prepare=%+v created=%v err=%v", first, created, err)
			}
			// JSONB reorders keys. Recovery must compare typed exact values,
			// not serialized bytes or a float64-rounded identifier.
			retry, created, err := prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, result.ID, request)
			if err != nil || created || !retry.CreatedAt.Equal(first.CreatedAt) {
				t.Fatalf("retry=%+v created=%v err=%v", retry, created, err)
			}
			for _, statement := range []struct {
				sql string
				id  uuid.UUID
			}{
				{"UPDATE catalog.responsibilities SET status = 'needs_transfer' WHERE id = ?", result.ResponsibilityID},
				{"UPDATE catalog.source_bindings SET source_version = '00000000000000000002' WHERE id = ?", result.SourceBindingID},
				{"UPDATE catalog.entries SET governance_status = 'deprecated' WHERE id = ?", entry.ID},
			} {
				err := tx.Transaction(func(change *gorm.DB) error { return change.Exec(statement.sql, statement.id).Error })
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.ConstraintName != "catalog_fulfillment_unresolved" {
					t.Fatalf("pending basis not protected: %v", err)
				}
			}
			if err := tx.Exec("UPDATE catalog.entries SET business_name = 'Pending prose edit', version = version + 1 WHERE id = ?", entry.ID).Error; err != nil {
				return err
			}
			changed := request
			changed.RequirementVersion++
			if _, _, err := prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, result.ID, changed); !errors.Is(err, ErrSharingDecisionConflict) {
				t.Fatalf("changed requirement reused request: %v", err)
			}
			// Failure of a later attempt rolls back only that attempt, not the
			// already persisted pending check in this owner fixture transaction.
			failure := errors.New("later consumer failure")
			if err := tx.Transaction(func(attempt *gorm.DB) error {
				if _, _, err := prepareSharingFulfillment(context.Background(), attempt, 7, entry.ID, result.ID, request); err != nil {
					return err
				}
				return failure
			}); !errors.Is(err, failure) {
				return err
			}
			var count int64
			if err := tx.Model(&models.FulfillmentCheck{}).Where("request_id = ?", first.RequestID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("later rollback erased pending: count=%d err=%v", count, err)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		var count int64
		if err := db.Model(&models.FulfillmentCheck{}).Where("catalog_entry_id = ?", entry.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("preparation rollback left pending: count=%d err=%v", count, err)
		}
	})
	t.Run("authority unsupported targets never freeze current basis", func(t *testing.T) {
		for name, mutate := range map[string]func(*plugin.EngineCatalogPath){
			"65 segments": func(path *plugin.EngineCatalogPath) {
				leaf := path.Segments[len(path.Segments)-1]
				path.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments[:1]...)
				for len(path.Segments) < 64 {
					path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "directory", Kind: "directory", Name: "node"})
				}
				path.Segments = append(path.Segments, leaf)
			},
			"encoded bytes exceed 16 KiB": func(path *plugin.EngineCatalogPath) { path.Segments[2].Name = strings.Repeat("甲", 6000) },
		} {
			t.Run(name, func(t *testing.T) {
				rollback := errors.New("rollback unsupported target fixture")
				err := db.Transaction(func(tx *gorm.DB) error {
					request := preparationFixture(t, result)
					mutate(&request.Path)
					// Seed a business decision under the unchanged Catalog history
					// constraints. No UPDATE, disabled trigger or remote source write:
					// a recorded business decision is not a consumable System target.
					decision := result.SharingDecision
					decision.ID, decision.EntryVersion = uuid.New(), result.EntryVersion+1
					encoded, err := json.Marshal(request.Path)
					if err != nil {
						return err
					}
					decision.CatalogPath = encoded
					if err := tx.Create(&decision).Error; err != nil {
						return err
					}
					if err := tx.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("version", decision.EntryVersion).Error; err != nil {
						return err
					}
					if check, created, err := prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, decision.ID, request); !errors.Is(err, ErrSharingDecisionConflict) || check != nil || created {
						t.Fatalf("unsupported target prepared: check=%+v created=%v err=%v", check, created, err)
					}
					var count int64
					if err := tx.Model(&models.FulfillmentCheck{}).Where("catalog_entry_id = ?", entry.ID).Count(&count).Error; err != nil || count != 0 {
						t.Fatalf("unsupported target left protection: count=%d err=%v", count, err)
					}
					if err := tx.Model(&models.Responsibility{}).Where("id = ?", decision.ResponsibilityID).Update("status", "needs_transfer").Error; err != nil {
						return err
					}
					return rollback
				})
				if !errors.Is(err, rollback) {
					t.Fatal(err)
				}
			})
		}
	})
	// Reconstruct the previous finite-only schema inside the rollback fixture.
	// Keep immutable protection active so migration must deliberately restore it.
	if err := db.Exec(`DROP TRIGGER guard_sharing_decision ON catalog.sharing_decisions;
		ALTER TABLE catalog.sharing_decisions DROP CONSTRAINT ck_sharing_decision_shape;
		ALTER TABLE catalog.sharing_decisions DROP COLUMN expiry_mode;
		ALTER TABLE catalog.sharing_decisions ALTER COLUMN expires_at SET NOT NULL;
		ALTER TABLE catalog.sharing_decisions ADD CONSTRAINT ck_sharing_decision_shape CHECK (isfinite(expires_at) AND expires_at > created_at);
		CREATE TRIGGER guard_sharing_decision BEFORE INSERT OR UPDATE OR DELETE ON catalog.sharing_decisions FOR EACH ROW EXECUTE FUNCTION catalog.guard_sharing_decision();`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := repository.Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var migrated models.SharingDecision
	if err := db.Where("id = ?", result.ID).Take(&migrated).Error; err != nil ||
		migrated.ExpiryMode != authorization.SharingExpiryAtTime || !migrated.ExpiresAt.Equal(*result.ExpiresAt) || !migrated.CreatedAt.Equal(result.CreatedAt) {
		t.Fatalf("finite history changed by migration: %+v err=%v", migrated, err)
	}
	for _, statement := range []string{
		"UPDATE catalog.sharing_decisions SET reason = 'changed' WHERE id = ?",
		"DELETE FROM catalog.sharing_decisions WHERE id = ?",
		"TRUNCATE catalog.sharing_decisions",
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if statement == "TRUNCATE catalog.sharing_decisions" {
				return tx.Exec(statement).Error
			}
			return tx.Exec(statement, result.ID).Error
		})
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "23514" {
			t.Fatalf("history mutation escaped: %s err=%v", statement, err)
		}
	}
	for _, path := range []string{`{}`, `{"version":"catalog.path/v1","engine_id":12,"segments":null}`, `{"version":"catalog.path/v1","engine_id":13,"segments":[{},{},{}]}`} {
		invalid := result.SharingDecision
		invalid.ID = uuid.New()
		invalid.EntryVersion++
		invalid.CatalogPath = json.RawMessage(path)
		err := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&invalid).Error })
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "23514" {
			t.Fatalf("invalid path accepted: %s err=%v", path, err)
		}
	}
	// Force a failure after decision insertion and aggregate version advancement.
	// The test-only constraint is rolled back with its enclosing savepoint.
	rollbackFixture := errors.New("rollback audit failure fixture")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`ALTER TABLE catalog.audit_events ADD CONSTRAINT sharing_decision_test_audit_failure CHECK (event_type <> 'catalog.sharing_decision.created') NOT VALID`).Error; err != nil {
			return err
		}
		attempt := input
		attempt.DecisionID, attempt.Version = uuid.New(), 2
		failing := NewEntryService(tx, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
		_, created, err := failing.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, attempt, auth)
		var pgError *pgconn.PgError
		if created || !errors.As(err, &pgError) || pgError.ConstraintName != "sharing_decision_test_audit_failure" {
			t.Fatalf("expected audit failure, created=%v err=%v", created, err)
		}
		var count int64
		if err := tx.Model(&models.SharingDecision{}).Where("id = ?", attempt.DecisionID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial decision=%d err=%v", count, err)
		}
		var unchanged models.Entry
		if err := tx.First(&unchanged, "id = ?", entry.ID).Error; err != nil || unchanged.Version != 2 {
			t.Fatalf("partial aggregate=%+v err=%v", unchanged, err)
		}
		return rollbackFixture
	}); !errors.Is(err, rollbackFixture) {
		t.Fatal(err)
	}
	// Removing current responsibility changes future qualification, not history.
	if err := db.Where("catalog_entry_id = ?", entry.ID).Delete(&models.Responsibility{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, result.ID, auth); err != nil {
		t.Fatal(err)
	}
	input.DecisionID, input.Version = uuid.New(), 2
	if _, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, auth); !errors.Is(err, ErrSharingConfirmationForbidden) {
		t.Fatalf("transferred owner retained confirmation: %v", err)
	}
	var count int64
	if err := db.Model(&models.SharingDecision{}).Where("catalog_entry_id = ?", entry.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("history count=%d err=%v", count, err)
	}
	if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.sharing_decision.created").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
	if err := db.First(&entry, "id = ?", entry.ID).Error; err != nil || entry.Version != 2 {
		t.Fatalf("aggregate=%+v err=%v", entry, err)
	}
	longEntry, longInput := seedSharingEntry(t, db)
	longInput.ExpiryMode, longInput.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
	longResult, created, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, longEntry.ID, longInput, auth)
	if err != nil || !created || longResult.ExpiresAt != nil || longResult.ExpiryMode != authorization.SharingExpiryUntilRevoked {
		t.Fatalf("long-term decision=%+v created=%v err=%v", longResult, created, err)
	}
	for _, invalid := range []models.SharingDecision{
		{ExpiryMode: "", ExpiresAt: nil},
		{ExpiryMode: authorization.SharingExpiryAtTime, ExpiresAt: nil},
		{ExpiryMode: authorization.SharingExpiryUntilRevoked, ExpiresAt: result.ExpiresAt},
	} {
		row := longResult.SharingDecision
		row.ID, row.EntryVersion = uuid.New(), row.EntryVersion+1
		row.ExpiryMode, row.ExpiresAt = invalid.ExpiryMode, invalid.ExpiresAt
		err := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&row).Error })
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "23514" {
			t.Fatalf("invalid persisted mode/date accepted: %+v err=%v", invalid, err)
		}
	}
}
