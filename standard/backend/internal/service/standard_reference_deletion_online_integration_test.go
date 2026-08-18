package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	commonconfig "github.com/addp/common/config"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const standardReferenceOnlineTestFlag = "ADDP_STANDARD_MODEL_ONLINE_TEST"

type failFirstDeletedTransport struct {
	base   http.RoundTripper
	failed atomic.Bool
}

func (t *failFirstDeletedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/standard-reference-guards/domain/") {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
		var payload struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if payload.State == commonclient.StandardReferenceGuardDeleted && t.failed.CompareAndSwap(false, true) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     http.StatusText(http.StatusServiceUnavailable),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":"injected deleted notification failure"}`)),
				Request:    request,
			}, nil
		}
	}
	return t.base.RoundTrip(request)
}

func TestOnlineStandardReferenceDeletionRejectsReferencesAndReconcilesFinalization(t *testing.T) {
	if os.Getenv(standardReferenceOnlineTestFlag) != "1" {
		t.Skip(standardReferenceOnlineTestFlag + " is not set to 1")
	}
	commonconfig.LoadEnv()

	tenantID := int64(1)
	if value := os.Getenv("ADDP_STANDARD_MODEL_ONLINE_TENANT_ID"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid ADDP_STANDARD_MODEL_ONLINE_TENANT_ID %q", value)
		}
		tenantID = parsed
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		commonconfig.GetEnv("POSTGRES_HOST", "localhost"),
		commonconfig.GetEnv("POSTGRES_PORT", "15432"),
		commonconfig.GetEnv("POSTGRES_USER", "addp"),
		commonconfig.GetEnv("POSTGRES_PASSWORD", "addp_password"),
		commonconfig.GetEnv("POSTGRES_DB", "addp"),
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open online PostgreSQL: %v", err)
	}

	systemURL := commonconfig.GetEnv("SYSTEM_URL", "http://localhost:8180")
	modelURL := commonconfig.GetEnv("MODEL_URL", "http://localhost:8181")
	tokenSource, err := commonclient.NewOAuthServiceTokenSource(
		systemURL,
		"addp-standard",
		os.Getenv("STANDARD_SERVICE_CLIENT_SECRET"),
		nil,
	)
	if err != nil {
		t.Fatalf("create Standard service token source: %v", err)
	}
	faultTransport := &failFirstDeletedTransport{base: http.DefaultTransport}
	modelClient := commonclient.NewModelClient(modelURL, tokenSource, &http.Client{
		Transport: faultTransport,
		Timeout:   10 * time.Second,
	})

	stamp := time.Now().UnixNano()
	domain := &models.Domain{
		TenantID:  tenantID,
		Name:      fmt.Sprintf("Reference deletion online %d", stamp),
		Code:      fmt.Sprintf("reference_deletion_online_%d", stamp),
		CreatedBy: 1,
	}
	if err := db.Create(domain).Error; err != nil {
		t.Fatalf("create online test domain: %v", err)
	}
	var entityID int64
	if err := db.Raw(`INSERT INTO model.entities
		(tenant_id, domain_id, name, code, description, status, version, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', 'draft', 1, 1, NOW(), NOW()) RETURNING id`,
		tenantID, domain.ID, fmt.Sprintf("Reference deletion entity %d", stamp), fmt.Sprintf("reference_deletion_entity_%d", stamp),
	).Scan(&entityID).Error; err != nil {
		t.Fatalf("create online test entity: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM model.entity_attributes WHERE entity_id = ?", entityID).Error
		_ = db.Exec("DELETE FROM model.entity_relations WHERE tenant_id = ? AND (source_entity = ? OR target_entity = ?)", tenantID, entityID, entityID).Error
		_ = db.Exec("DELETE FROM model.entities WHERE id = ? AND tenant_id = ?", entityID, tenantID).Error
		_ = db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, "domain", domain.ID).
			Delete(&models.StandardReferenceDeletion{}).Error
		_ = db.Exec("DELETE FROM model.standard_reference_guards WHERE tenant_id = ? AND resource_type = 'domain' AND resource_id = ?", tenantID, domain.ID).Error
		_ = db.Where("id = ? AND tenant_id = ?", domain.ID, tenantID).Delete(&models.Domain{}).Error
	})

	domainRepo := repository.NewDomainRepository(db)
	svc := NewStandardReferenceDeletionService(db, modelClient)
	svc.RegisterLocalDelete("domain", domainRepo.DeleteTx)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = svc.Delete(ctx, tenantID, "domain", domain.ID, domainRepo.DeleteTx)
	var referenced *StandardResourceReferencedError
	if !errors.As(err, &referenced) || referenced.Impact == nil || referenced.Impact.ReferenceCount != 1 {
		t.Fatalf("referenced delete error = %#v, want one Model reference", err)
	}
	assertOnlineReferenceDeletionState(t, db, tenantID, domain.ID, standardLifecycleActive, 1, "open")

	if err := db.Exec("DELETE FROM model.entities WHERE id = ? AND tenant_id = ?", entityID, tenantID).Error; err != nil {
		t.Fatalf("delete online test entity reference: %v", err)
	}
	err = svc.Delete(ctx, tenantID, "domain", domain.ID, domainRepo.DeleteTx)
	if !errors.Is(err, ErrModelReferenceGuardUnavailable) || !faultTransport.failed.Load() {
		t.Fatalf("delete after injected finalization failure = %v, injected=%t", err, faultTransport.failed.Load())
	}
	assertOnlineReferenceDeletionState(t, db, tenantID, domain.ID, "", 0, "frozen")

	if err := db.Model(&models.StandardReferenceDeletion{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, "domain", domain.ID).
		Update("next_attempt_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("make online deletion reconciliation due: %v", err)
	}
	svc.reconcilePending(ctx)
	assertOnlineReferenceDeletionState(t, db, tenantID, domain.ID, "", 0, "deleted")
}

func assertOnlineReferenceDeletionState(
	t *testing.T,
	db *gorm.DB,
	tenantID, domainID int64,
	wantLifecycle string,
	wantDomainCount int64,
	wantGuardState string,
) {
	t.Helper()
	var domainCount int64
	if err := db.Model(&models.Domain{}).Where("id = ? AND tenant_id = ?", domainID, tenantID).Count(&domainCount).Error; err != nil {
		t.Fatalf("count online test domain: %v", err)
	}
	if domainCount != wantDomainCount {
		t.Fatalf("online test domain count = %d, want %d", domainCount, wantDomainCount)
	}
	if wantDomainCount > 0 {
		var lifecycle string
		if err := db.Model(&models.Domain{}).Select("lifecycle_state").Where("id = ? AND tenant_id = ?", domainID, tenantID).Scan(&lifecycle).Error; err != nil {
			t.Fatalf("load online test domain lifecycle: %v", err)
		}
		if lifecycle != wantLifecycle {
			t.Fatalf("online test domain lifecycle = %q, want %q", lifecycle, wantLifecycle)
		}
	}
	var guardState string
	if err := db.Raw(`SELECT state FROM model.standard_reference_guards
		WHERE tenant_id = ? AND resource_type = 'domain' AND resource_id = ?`, tenantID, domainID).Scan(&guardState).Error; err != nil {
		t.Fatalf("load online test Model guard: %v", err)
	}
	if guardState != wantGuardState {
		t.Fatalf("online test Model guard = %q, want %q", guardState, wantGuardState)
	}
	var operationCount int64
	if err := db.Model(&models.StandardReferenceDeletion{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, "domain", domainID).
		Count(&operationCount).Error; err != nil {
		t.Fatalf("count online deletion operations: %v", err)
	}
	wantOperationCount := int64(0)
	if wantGuardState == "frozen" && wantDomainCount == 0 {
		wantOperationCount = 1
	}
	if operationCount != wantOperationCount {
		t.Fatalf("online deletion operation count = %d, want %d", operationCount, wantOperationCount)
	}
}
