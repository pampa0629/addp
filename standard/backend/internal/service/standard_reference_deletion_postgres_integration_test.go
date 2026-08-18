package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openStandardReferenceDeletionPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("run Standard migrations: %v", err)
	}
	return db
}

func waitForStandardDeletionLock(t *testing.T, db *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		err := db.Raw(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND (query LIKE '%reference_deletions%' OR query LIKE '%standard.domains%')`).Scan(&count).Error
		if err != nil {
			t.Fatalf("inspect PostgreSQL lock wait: %v", err)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Standard deletion coordination lock")
}

func TestPostgresStandardReferenceDeletionSerializesConcurrentDeletes(t *testing.T) {
	db := openStandardReferenceDeletionPostgres(t)
	tenantID := time.Now().UnixNano()
	domain := &models.Domain{
		TenantID: tenantID, Name: "Concurrent deletion", Code: fmt.Sprintf("concurrent_delete_%d", tenantID), CreatedBy: 1,
	}
	if err := db.Create(domain).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.StandardReferenceDeletion{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Domain{}).Error
	})

	freezeEntered := make(chan struct{})
	releaseFreeze := make(chan struct{})
	var freezeOnce sync.Once
	guardState := commonclient.StandardReferenceGuardOpen
	var guardMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode guard request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.State == commonclient.StandardReferenceGuardFrozen {
			freezeOnce.Do(func() {
				close(freezeEntered)
				<-releaseFreeze
			})
		}
		guardMu.Lock()
		guardState = request.State
		state := guardState
		guardMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"resource_type": "domain", "resource_id": domain.ID, "state": state,
			"reference_count": 0, "summary": []interface{}{}, "sample": []interface{}{}, "sample_truncated": false,
		})
	}))
	defer server.Close()

	client := commonclient.NewModelClient(server.URL, commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "standard-runtime-token", nil
	}), server.Client())
	svc := NewStandardReferenceDeletionService(db, client)
	domainRepo := repository.NewDomainRepository(db)
	var deleteCalls atomic.Int32
	deleteLocal := func(tx *gorm.DB, resourceID, resourceTenantID int64) error {
		deleteCalls.Add(1)
		return domainRepo.DeleteTx(tx, resourceID, resourceTenantID)
	}

	results := make(chan error, 2)
	go func() { results <- svc.Delete(context.Background(), tenantID, "domain", domain.ID, deleteLocal) }()
	<-freezeEntered
	go func() { results <- svc.Delete(context.Background(), tenantID, "domain", domain.ID, deleteLocal) }()
	waitForStandardDeletionLock(t, db)
	close(releaseFreeze)

	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent delete: %v", err)
		}
	}
	if calls := deleteCalls.Load(); calls != 1 {
		t.Fatalf("local delete calls = %d, want 1", calls)
	}
	var operationCount int64
	if err := db.Model(&models.StandardReferenceDeletion{}).Where("tenant_id = ?", tenantID).Count(&operationCount).Error; err != nil {
		t.Fatalf("count deletion operations: %v", err)
	}
	if operationCount != 0 {
		t.Fatalf("deletion operation count = %d, want 0", operationCount)
	}
}

func TestPostgresStandardReferenceDeletionRestoresAfterForeignKeyFailure(t *testing.T) {
	db := openStandardReferenceDeletionPostgres(t)
	tenantID := time.Now().UnixNano()
	parent := &models.Domain{
		TenantID: tenantID, Name: "Parent domain", Code: fmt.Sprintf("parent_domain_%d", tenantID), CreatedBy: 1,
	}
	if err := db.Create(parent).Error; err != nil {
		t.Fatalf("create parent domain: %v", err)
	}
	child := &models.Domain{
		TenantID: tenantID, ParentID: &parent.ID, Name: "Child domain", Code: fmt.Sprintf("child_domain_%d", tenantID), CreatedBy: 1,
	}
	if err := db.Create(child).Error; err != nil {
		t.Fatalf("create child domain: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.StandardReferenceDeletion{}).Error
		_ = db.Where("id = ? AND tenant_id = ?", child.ID, tenantID).Delete(&models.Domain{}).Error
		_ = db.Where("id = ? AND tenant_id = ?", parent.ID, tenantID).Delete(&models.Domain{}).Error
	})

	guardState := commonclient.StandardReferenceGuardOpen
	var guardMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode guard request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		guardMu.Lock()
		guardState = request.State
		state := guardState
		guardMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"resource_type": "domain", "resource_id": parent.ID, "state": state,
			"reference_count": 0, "summary": []interface{}{}, "sample": []interface{}{}, "sample_truncated": false,
		})
	}))
	defer server.Close()

	client := commonclient.NewModelClient(server.URL, commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "standard-runtime-token", nil
	}), server.Client())
	svc := NewStandardReferenceDeletionService(db, client)
	domainRepo := repository.NewDomainRepository(db)
	err := svc.Delete(context.Background(), tenantID, "domain", parent.ID, domainRepo.DeleteTx)
	if err == nil {
		t.Fatal("delete parent domain succeeded, want foreign key failure")
	}

	var restored models.Domain
	if err := db.Where("id = ? AND tenant_id = ?", parent.ID, tenantID).First(&restored).Error; err != nil {
		t.Fatalf("load restored parent domain: %v", err)
	}
	if restored.LifecycleState != standardLifecycleActive {
		t.Fatalf("parent lifecycle state = %q, want %q", restored.LifecycleState, standardLifecycleActive)
	}
	var operationCount int64
	if err := db.Model(&models.StandardReferenceDeletion{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, "domain", parent.ID).
		Count(&operationCount).Error; err != nil {
		t.Fatalf("count deletion operations: %v", err)
	}
	if operationCount != 0 {
		t.Fatalf("deletion operation count = %d, want 0", operationCount)
	}
	guardMu.Lock()
	finalGuardState := guardState
	guardMu.Unlock()
	if finalGuardState != commonclient.StandardReferenceGuardOpen {
		t.Fatalf("model guard state = %q, want %q", finalGuardState, commonclient.StandardReferenceGuardOpen)
	}
}
