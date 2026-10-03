package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	"github.com/addp/common/authorization"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresSharingFulfillmentRecovery(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable Catalog PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS catalog CASCADE").Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := db.Raw("SELECT count(*) FROM pg_namespace WHERE nspname = 'catalog'").Scan(&count).Error; err != nil || count != 0 {
			t.Errorf("Catalog residual=%d err=%v", count, err)
		}
	})
	if err := db.Exec("DROP SCHEMA IF EXISTS catalog CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	exerciseSharingFulfillmentPreparation(t, db)
	exerciseSharingFulfillmentHistory(t, db)
	entry, input := seedSharingEntry(t, db)
	input.ExpiryMode, input.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	decision, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil {
		t.Fatal(err)
	}
	request := preparationFixture(t, decision)
	var check *models.FulfillmentCheck
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		check, _, err = prepareSharingFulfillment(context.Background(), tx, 7, entry.ID, decision.ID, request)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	binding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		t.Fatal(err)
	}
	changeBasis := func() error {
		return db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&models.Responsibility{}).Where("id = ?", decision.ResponsibilityID).Update("status", "needs_transfer").Error
		})
	}
	assertProtected := func() {
		t.Helper()
		var pgErr *pgconn.PgError
		if err := changeBasis(); !errors.As(err, &pgErr) || pgErr.ConstraintName != "catalog_fulfillment_unresolved" {
			t.Fatalf("pending protection lost: %v", err)
		}
	}
	assertProtected()
	exerciseCatalogRuntimeRecovery(t, db, check)
	rollback := errors.New("local completion interrupted")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := resolveSharingFulfillment(context.Background(), tx, 7, entry.ID, check.RequestID, binding); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	assertProtected()

	authority := &fakeSharingFulfillmentAuthority{
		read: func(int64, uuid.UUID, sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
			return nil, errSharingFulfillmentNotFound
		},
		close: func(int64, uuid.UUID, sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
			return nil, errors.New("close response lost")
		},
	}
	if result, err := reconcileSharingFulfillment(context.Background(), db, 7, entry.ID, check.RequestID, authority); err == nil || result != nil {
		t.Fatalf("unknown authority result resolved: %+v err=%v", result, err)
	}
	assertProtected()
	// A committed outcome can be recovered after a lost response. Concurrent
	// local completion must return the same original time, not update twice.
	started, release := make(chan struct{}), make(chan struct{})
	authority.close = func(tenant int64, id uuid.UUID, actual sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
		close(started)
		<-release
		return &sharingFulfillmentResolution{id, tenant, actual, "accepted"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := make(chan *models.FulfillmentCheck, 1)
	errorsCh := make(chan error, 1)
	go func() {
		row, err := reconcileSharingFulfillment(ctx, db, 7, entry.ID, check.RequestID, authority)
		result <- row
		errorsCh <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		close(release)
		t.Fatal("authority call did not start")
	}
	// Remote call is outside any Catalog transaction: a different connection
	// can acquire the entry's write lock and commit while the adapter waits.
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("business_name", "During authority call").Error
	})
	close(release)
	if err != nil {
		t.Fatalf("owner lock held during remote call: %v", err)
	}
	resolved := <-result
	if err := <-errorsCh; err != nil || resolved == nil || resolved.ResolvedAt == nil {
		t.Fatalf("recovery=%+v err=%v", resolved, err)
	}
	// Two independent workers completing another pending request contend on
	// the same entry/check boundary and retain one database-clock timestamp.
	concurrent := request
	concurrent.RequestID = uuid.New()
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, err := prepareSharingFulfillment(ctx, tx, 7, entry.ID, decision.ID, concurrent)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	type completion struct {
		row *models.FulfillmentCheck
		err error
	}
	completions := make(chan completion, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			var row *models.FulfillmentCheck
			err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var err error
				row, err = resolveSharingFulfillment(ctx, tx, 7, entry.ID, concurrent.RequestID, binding)
				return err
			})
			completions <- completion{row, err}
		}()
	}
	close(start)
	first, second := <-completions, <-completions
	if first.err != nil || second.err != nil || first.row == nil || second.row == nil ||
		first.row.ResolvedAt == nil || second.row.ResolvedAt == nil || !first.row.ResolvedAt.Equal(*second.row.ResolvedAt) {
		t.Fatalf("concurrent completion: %+v / %+v", first, second)
	}
	if err := changeBasis(); err != nil {
		t.Fatalf("committed recovery did not release basis: %v", err)
	}
	// Another resolver and a history retry preserve immutable JSONB/time even
	// after the original responsibility becomes unavailable.
	if err := db.Transaction(func(tx *gorm.DB) error {
		row, err := resolveSharingFulfillment(context.Background(), tx, 7, entry.ID, check.RequestID, binding)
		if err == nil && !row.ResolvedAt.Equal(*resolved.ResolvedAt) {
			return errors.New("repeated completion refreshed time")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	retry, err := reconcileSharingFulfillment(context.Background(), db, 7, entry.ID, check.RequestID, authority)
	if err != nil || !retry.ResolvedAt.Equal(*resolved.ResolvedAt) || authority.closeCalls != 2 || !retry.CreatedAt.Equal(check.CreatedAt) {
		t.Fatalf("retry=%+v calls=%d err=%v", retry, authority.closeCalls, err)
	}
}
