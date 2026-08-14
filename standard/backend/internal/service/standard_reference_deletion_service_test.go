package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupStandardReferenceDeletionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.domains (
		id INTEGER PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		lifecycle_state TEXT NOT NULL DEFAULT 'active',
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create domains: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.domains (id, tenant_id) VALUES (42, 7)`).Error; err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	return db
}

func newStandardReferenceDeletionTestService(
	t *testing.T,
	db *gorm.DB,
	handler http.HandlerFunc,
) (*StandardReferenceDeletionService, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := commonclient.NewModelClient(
		server.URL,
		commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "standard-runtime-token", nil
		}),
		server.Client(),
	)
	return NewStandardReferenceDeletionService(db, client), server
}

func readGuardState(t *testing.T, r *http.Request) string {
	t.Helper()
	if r.Method != http.MethodPut || r.URL.Path != "/api/v1/model/standard-reference-guards/domain/42" {
		t.Fatalf("unexpected Model request: %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer standard-runtime-token" {
		t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
	}
	var request struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatalf("decode guard request: %v", err)
	}
	return request.State
}

func writeGuardResponse(t *testing.T, w http.ResponseWriter, state string, referenceCount int64) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"resource_type":    "domain",
		"resource_id":      42,
		"state":            state,
		"reference_count":  referenceCount,
		"summary":          []interface{}{},
		"sample":           []interface{}{},
		"sample_truncated": false,
	}); err != nil {
		t.Errorf("encode guard response: %v", err)
	}
}

func domainLifecycleState(t *testing.T, db *gorm.DB) (string, int64) {
	t.Helper()
	var state string
	var count int64
	if err := db.Raw("SELECT lifecycle_state FROM standard.domains WHERE id = 42 AND tenant_id = 7").Scan(&state).Error; err != nil {
		t.Fatalf("load domain lifecycle: %v", err)
	}
	if err := db.Table("standard.domains").Where("id = 42 AND tenant_id = 7").Count(&count).Error; err != nil {
		t.Fatalf("count domain: %v", err)
	}
	return state, count
}

func TestStandardReferenceDeletionRestoresReferencedResource(t *testing.T) {
	db := setupStandardReferenceDeletionTestDB(t)
	states := []string{}
	svc, server := newStandardReferenceDeletionTestService(t, db, func(w http.ResponseWriter, r *http.Request) {
		state := readGuardState(t, r)
		states = append(states, state)
		if state == commonclient.StandardReferenceGuardFrozen {
			writeGuardResponse(t, w, state, 2)
			return
		}
		writeGuardResponse(t, w, state, 0)
	})
	defer server.Close()

	deleteCalled := false
	err := svc.Delete(context.Background(), 7, "domain", 42, func() error {
		deleteCalled = true
		return nil
	})
	var referenced *StandardResourceReferencedError
	if !errors.As(err, &referenced) || referenced.Impact.ReferenceCount != 2 {
		t.Fatalf("delete error = %#v, want referenced impact", err)
	}
	if deleteCalled {
		t.Fatal("local delete called for referenced resource")
	}
	state, count := domainLifecycleState(t, db)
	if state != standardLifecycleActive || count != 1 {
		t.Fatalf("domain state=%q count=%d, want active and present", state, count)
	}
	if !reflect.DeepEqual(states, []string{commonclient.StandardReferenceGuardFrozen, commonclient.StandardReferenceGuardOpen}) {
		t.Fatalf("guard states = %v", states)
	}
}

func TestStandardReferenceDeletionDeletesUnreferencedResource(t *testing.T) {
	db := setupStandardReferenceDeletionTestDB(t)
	states := []string{}
	svc, server := newStandardReferenceDeletionTestService(t, db, func(w http.ResponseWriter, r *http.Request) {
		state := readGuardState(t, r)
		states = append(states, state)
		writeGuardResponse(t, w, state, 0)
	})
	defer server.Close()

	err := svc.Delete(context.Background(), 7, "domain", 42, func() error {
		return db.Exec("DELETE FROM standard.domains WHERE id = 42 AND tenant_id = 7").Error
	})
	if err != nil {
		t.Fatalf("delete resource: %v", err)
	}
	_, count := domainLifecycleState(t, db)
	if count != 0 {
		t.Fatalf("domain count = %d, want 0", count)
	}
	if !reflect.DeepEqual(states, []string{commonclient.StandardReferenceGuardFrozen, commonclient.StandardReferenceGuardDeleted}) {
		t.Fatalf("guard states = %v", states)
	}
}

func TestStandardReferenceDeletionDoesNotDeleteWhenModelUnavailable(t *testing.T) {
	db := setupStandardReferenceDeletionTestDB(t)
	svc, server := newStandardReferenceDeletionTestService(t, db, func(w http.ResponseWriter, r *http.Request) {
		_ = readGuardState(t, r)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer server.Close()

	deleteCalled := false
	err := svc.Delete(context.Background(), 7, "domain", 42, func() error {
		deleteCalled = true
		return nil
	})
	if !errors.Is(err, ErrModelReferenceGuardUnavailable) {
		t.Fatalf("delete error = %v, want ErrModelReferenceGuardUnavailable", err)
	}
	if deleteCalled {
		t.Fatal("local delete called while Model was unavailable")
	}
	state, count := domainLifecycleState(t, db)
	if state != standardLifecycleActive || count != 1 {
		t.Fatalf("domain state=%q count=%d, want active and present", state, count)
	}
}

func TestStandardReferenceDeletionRestoresAfterLocalDeleteFailure(t *testing.T) {
	db := setupStandardReferenceDeletionTestDB(t)
	states := []string{}
	svc, server := newStandardReferenceDeletionTestService(t, db, func(w http.ResponseWriter, r *http.Request) {
		state := readGuardState(t, r)
		states = append(states, state)
		writeGuardResponse(t, w, state, 0)
	})
	defer server.Close()

	deleteFailure := errors.New("local foreign key conflict")
	err := svc.Delete(context.Background(), 7, "domain", 42, func() error { return deleteFailure })
	if !errors.Is(err, deleteFailure) {
		t.Fatalf("delete error = %v, want local failure", err)
	}
	state, count := domainLifecycleState(t, db)
	if state != standardLifecycleActive || count != 1 {
		t.Fatalf("domain state=%q count=%d, want active and present", state, count)
	}
	if !reflect.DeepEqual(states, []string{commonclient.StandardReferenceGuardFrozen, commonclient.StandardReferenceGuardOpen}) {
		t.Fatalf("guard states = %v", states)
	}
}

func TestStandardReferenceDeletionRetryFinalizesAfterLocalResourceWasDeleted(t *testing.T) {
	db := setupStandardReferenceDeletionTestDB(t)
	states := []string{}
	deletedAttempts := 0
	svc, server := newStandardReferenceDeletionTestService(t, db, func(w http.ResponseWriter, r *http.Request) {
		state := readGuardState(t, r)
		states = append(states, state)
		if state == commonclient.StandardReferenceGuardDeleted {
			deletedAttempts++
			if deletedAttempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		writeGuardResponse(t, w, state, 0)
	})
	defer server.Close()

	deleteCalls := 0
	deleteLocal := func() error {
		deleteCalls++
		return db.Exec("DELETE FROM standard.domains WHERE id = 42 AND tenant_id = 7").Error
	}
	if err := svc.Delete(context.Background(), 7, "domain", 42, deleteLocal); !errors.Is(err, ErrModelReferenceGuardUnavailable) {
		t.Fatalf("first delete error = %v, want finalization failure", err)
	}
	if err := svc.Delete(context.Background(), 7, "domain", 42, deleteLocal); err != nil {
		t.Fatalf("retry finalization: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("local delete calls = %d, want 1", deleteCalls)
	}
	if !reflect.DeepEqual(states, []string{
		commonclient.StandardReferenceGuardFrozen,
		commonclient.StandardReferenceGuardDeleted,
		commonclient.StandardReferenceGuardDeleted,
	}) {
		t.Fatalf("guard states = %v", states)
	}
}

func TestStandardReferenceDeletionPreservesNotFoundWhenNoFrozenGuardExists(t *testing.T) {
	db := setupStandardReferenceDeletionTestDB(t)
	if err := db.Exec("DELETE FROM standard.domains WHERE id = 42").Error; err != nil {
		t.Fatalf("delete seed domain: %v", err)
	}
	svc, server := newStandardReferenceDeletionTestService(t, db, func(w http.ResponseWriter, r *http.Request) {
		if state := readGuardState(t, r); state != commonclient.StandardReferenceGuardDeleted {
			t.Fatalf("guard state = %q, want deleted", state)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"state conflict","error_code":"standard_reference_guard_state_conflict"}`))
	})
	defer server.Close()

	err := svc.Delete(context.Background(), 7, "domain", 42, func() error {
		t.Fatal("local delete called for missing resource")
		return nil
	})
	if !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("delete error = %v, want common not found", err)
	}
}
