package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type historyScopeReader struct {
	operator authorization.SharingFulfillmentOperator
	denied   map[int64]bool
	before   func()
	err      error
	calls    []int64
}

func (r *historyScopeReader) GetEngineAccessHandlingScope(_ context.Context, engine int64, token string) (*authorization.EngineAccessHandlingScope, error) {
	if token != "addp_at_handler" {
		return nil, errors.New("substituted human token")
	}
	r.calls = append(r.calls, engine)
	if r.before != nil {
		r.before()
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.denied[engine] {
		return nil, &client.SystemAPIError{StatusCode: 403}
	}
	return &authorization.EngineAccessHandlingScope{TenantID: 7, EngineID: engine, Operator: r.operator, VerifiedAt: time.Now().UTC()}, nil
}

func seedFulfillmentHistory(t *testing.T, db *gorm.DB) (models.Entry, *models.FulfillmentCheck, authorization.SharingFulfillmentBinding) {
	t.Helper()
	entry, input := seedSharingEntry(t, db)
	// Establish visibility before preparing: a pending check must continue to
	// protect governance changes in PostgreSQL, even inside a test fixture.
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]any{
		"visibility": models.VisibilityTenant, "governance_status": models.GovernanceStatusCurated,
	}).Error; err != nil {
		t.Fatal(err)
	}
	input.ExpiryMode, input.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	decision, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil {
		t.Fatal(err)
	}
	request := preparationFixture(t, decision)
	request.Operator.AuthorizationVersion = 9007199254740993
	request.RequirementVersion = 9007199254740993
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
	return entry, check, binding
}

func TestSharingFulfillmentHistoryIsOwnCurrentAndReadOnly(t *testing.T) {
	exerciseSharingFulfillmentHistory(t, openSharingPreparationTestDB(t))
}

func exerciseSharingFulfillmentHistory(t *testing.T, db *gorm.DB) {
	t.Run("own paging filters original engine scopes before count", func(t *testing.T) {
		entry, check, binding := seedFulfillmentHistory(t, db)
		auth := authtest.NewTenantUserAuthContext("7", "50", []string{"catalog.entry.read", "catalog.inventory.read", sourceHandlingPermission})
		p, m, v, _ := sharingUserProvenance(auth, 7, "catalog.entry.read", sourceHandlingPermission)
		scope := &historyScopeReader{operator: authorization.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}, denied: map[int64]bool{13: true}}
		s := NewEntryService(db, nil, nil).WithSharingHandlingScopeReader(scope)
		for _, fixture := range []struct {
			principal int64
			engine    uint
		}{{50, 13}, {51, 12}} {
			other := binding
			other.Operator.PrincipalID, other.Path.EngineID = fixture.principal, fixture.engine
			raw, _ := json.Marshal(other)
			if err := db.Create(&models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, RequestBinding: raw}).Error; err != nil {
				t.Fatal(err)
			}
		}
		rows, total, err := s.ListSharingFulfillments(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 1, 1)
		if err != nil || total != 1 || len(rows) != 1 || rows[0].RequestID != check.RequestID || len(scope.calls) != 2 || scope.calls[0] != 12 || scope.calls[1] != 13 {
			t.Fatalf("scope-filtered page=%+v total=%d calls=%v err=%v", rows, total, scope.calls, err)
		}
		body, _ := json.Marshal(rows)
		for _, forbidden := range []string{"caller_principal_id", "operator", "authorization_version", "resolved_at", "reason", "state"} {
			if strings.Contains(string(body), `"`+forbidden+`"`) {
				t.Fatalf("private/result field %s exposed: %s", forbidden, body)
			}
		}
		if !strings.Contains(string(body), `"requirement_version":"9007199254740993"`) {
			t.Fatal("requirement version lost string precision")
		}
		rows, total, err = s.ListSharingFulfillments(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 2, 1)
		if err != nil || total != 1 || len(rows) != 0 {
			t.Fatalf("page past filtered total=%d rows=%v err=%v", total, rows, err)
		}
		scope.denied[13] = false
		rows, total, err = s.ListSharingFulfillments(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 2, 1)
		if err != nil || total != 2 || len(rows) != 1 {
			t.Fatalf("second managed page=%+v total=%d err=%v", rows, total, err)
		}
		for _, invalid := range [][2]int{{0, 20}, {1, 101}, {1, 0}} {
			if _, _, err := s.ListSharingFulfillments(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", invalid[0], invalid[1]); !errors.Is(err, ErrInvalidEntryUpdate) {
				t.Fatalf("invalid pagination %v: %v", invalid, err)
			}
		}
		scope.err = errors.New("scope service offline")
		if _, _, err := s.ListSharingFulfillments(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, auth, "addp_at_handler", 1, 20); !errors.Is(err, ErrReferenceValidationUnavailable) {
			t.Fatalf("dependency failure became empty history: %v", err)
		}
	})

	t.Run("read pending accepted closed without changing local history", func(t *testing.T) {
		entry, check, binding := seedFulfillmentHistory(t, db)
		auth := authtest.NewTenantUserAuthContext("7", "50", []string{"catalog.entry.read", "catalog.inventory.read", sourceHandlingPermission})
		p, m, v, _ := sharingUserProvenance(auth, 7, "catalog.entry.read", sourceHandlingPermission)
		scope := &historyScopeReader{operator: authorization.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}, denied: map[int64]bool{}}
		s := NewEntryService(db, nil, nil).WithSharingHandlingScopeReader(scope)
		state, failure := "pending", ""
		var before func()
		calls := 0
		recorded := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
		deadline := recorded.Add(5 * time.Minute) // Historical expiry does not become a new window.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/runtime/engine-access-fulfillments/"+check.RequestID.String()+"/resolve" || r.Header.Get("Authorization") != "Bearer fixture-catalog-runtime" || r.Header.Get("X-Tenant-ID") != "" {
				t.Errorf("unexpected mutation/credential/target: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(500)
				return
			}
			var actual authorization.SharingFulfillmentBinding
			if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || !equalSharingFulfillmentBinding(actual, binding) {
				t.Errorf("original operator or parameters replaced: %+v %v", actual, err)
			}
			if db.Dialector.Name() == "postgres" {
				if err := db.Transaction(func(tx *gorm.DB) error {
					return tx.Exec("SELECT id FROM catalog.entries WHERE id=? FOR UPDATE NOWAIT", entry.ID).Error
				}); err != nil {
					t.Errorf("read held a local entry lock across HTTP: %v", err)
				}
			}
			if before != nil {
				before()
			}
			if failure == "offline" {
				w.WriteHeader(503)
				return
			}
			lookup := authorization.SharingFulfillmentLookup{Found: state != "pending"}
			if lookup.Found {
				resolution := &authorization.SharingFulfillmentResolution{RequestID: check.RequestID, TenantID: 7, Binding: actual, Outcome: state, RecordedAt: recorded}
				if state == "accepted" {
					resolution.Deadline = &deadline
				}
				switch failure {
				case "binding":
					resolution.Binding.RequirementVersion++
				case "tenant":
					resolution.TenantID++
				case "request":
					resolution.RequestID = uuid.New()
				case "window":
					bad := recorded.Add(6 * time.Minute)
					resolution.Deadline = &bad
				}
				lookup.Resolution = resolution
			}
			_ = json.NewEncoder(w).Encode(lookup)
		}))
		defer server.Close()
		s.WithSharingFulfillmentClient(client.NewSystemFulfillmentClient(server.URL, recoveryRuntimeTestTokens{}, server.Client()))
		get := func() (*SharingFulfillmentHistory, error) {
			return s.GetSharingFulfillment(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, check.RequestID, auth, "addp_at_handler")
		}
		var auditBefore int64
		if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ?", entry.ID).Count(&auditBefore).Error; err != nil {
			t.Fatal(err)
		}
		for _, outcome := range []string{"pending", "accepted", "closed"} {
			state = outcome
			result, err := get()
			if err != nil || result.State != outcome || result.RequestID != check.RequestID || result.RequirementVersion != binding.RequirementVersion || result.Target.EngineID != "12" || !result.CreatedAt.Equal(check.CreatedAt) {
				t.Fatalf("historical %s=%+v err=%v", outcome, result, err)
			}
			if outcome == "accepted" && (result.Deadline == nil || !result.Deadline.Equal(deadline) || result.RecordedAt == nil || !result.RecordedAt.Equal(recorded)) {
				t.Fatal("historical window refreshed")
			}
			var stored models.FulfillmentCheck
			if err := db.Where("request_id=?", check.RequestID).Take(&stored).Error; err != nil || stored.ResolvedAt != nil || !stored.CreatedAt.Equal(check.CreatedAt) {
				t.Fatalf("GET settled history: %+v %v", stored, err)
			}
		}
		var auditAfter int64
		if err := db.Model(&models.AuditEvent{}).Where("catalog_entry_id = ?", entry.ID).Count(&auditAfter).Error; err != nil {
			t.Fatal(err)
		}
		if auditAfter != auditBefore {
			t.Fatal("read appended a business audit")
		}
		var requests int64
		if err := db.Model(&models.FulfillmentCheck{}).Where("catalog_entry_id = ?", entry.ID).Count(&requests).Error; err != nil || requests != 1 {
			t.Fatalf("read created requests: %d %v", requests, err)
		}
		state = "accepted"
		for _, bad := range []string{"offline", "binding", "tenant", "request", "window"} {
			failure = bad
			if result, err := get(); result != nil || !errors.Is(err, ErrReferenceValidationUnavailable) {
				t.Fatalf("invalid %s=%+v %v", bad, result, err)
			}
		}
		failure = ""
		beforeCalls := calls
		other := authtest.NewTenantUserAuthContext("7", "51", []string{"catalog.entry.read", "catalog.inventory.read", sourceHandlingPermission})
		if _, err := s.GetSharingFulfillment(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, check.RequestID, other, "addp_at_handler"); !errors.Is(err, ErrEntryNotFound) {
			t.Fatalf("other handler history leaked: %v", err)
		}
		cross := authtest.NewTenantUserAuthContext("8", "50", []string{"catalog.entry.read", "catalog.inventory.read", sourceHandlingPermission})
		if _, err := s.GetSharingFulfillment(context.Background(), 8, EntryAccess{Inventory: true}, entry.ID, check.RequestID, cross, "addp_at_handler"); !errors.Is(err, ErrEntryNotFound) {
			t.Fatalf("cross tenant history leaked: %v", err)
		}
		for _, perms := range [][]string{{"catalog.entry.read", "catalog.entry.update"}, {sourceHandlingPermission}} {
			denied := authtest.NewTenantUserAuthContext("7", "50", perms)
			if _, err := s.GetSharingFulfillment(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, check.RequestID, denied, "addp_at_handler"); !errors.Is(err, ErrSharingHandlingForbidden) {
				t.Fatalf("permission substitution: %v", err)
			}
		}
		machine := auth
		machine.Principal.Type = "service_principal"
		if _, err := s.GetSharingFulfillment(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, check.RequestID, machine, "addp_at_handler"); !errors.Is(err, ErrSharingHandlingForbidden) {
			t.Fatalf("machine accepted as human: %v", err)
		}
		if calls != beforeCalls {
			t.Fatal("denied request reached runtime resolve")
		}
		before = func() { scope.denied[12] = true }
		if _, err := get(); !errors.Is(err, ErrSharingHandlingForbidden) {
			t.Fatalf("scope withdrawn during resolve: %v", err)
		}
		before, scope.denied[12] = nil, false
		scope.operator.AuthorizationVersion++
		if _, err := get(); !errors.Is(err, ErrReferenceValidationUnavailable) {
			t.Fatalf("wrong current scope version: %v", err)
		}
		scope.operator.AuthorizationVersion--
		before = func() {
			if err := db.Model(&models.Entry{}).Where("id=?", entry.ID).Update("visibility", models.VisibilityDepartment).Error; err != nil {
				t.Fatal(err)
			}
		}
		// Without inventory the entry was tenant visible, then is hidden in HTTP.
		if err := db.Model(&models.Entry{}).Where("id=?", entry.ID).Updates(map[string]any{"visibility": models.VisibilityTenant, "governance_status": models.GovernanceStatusCurated}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetSharingFulfillment(context.Background(), 7, EntryAccess{}, entry.ID, check.RequestID, auth, "addp_at_handler"); !errors.Is(err, ErrEntryNotFound) {
			t.Fatalf("visibility changed during resolve: %v", err)
		}
		before = nil
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, err := resolveSharingFulfillment(context.Background(), tx, 7, entry.ID, check.RequestID, binding)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.Entry{}).Where("id=?", entry.ID).Update("governance_status", models.GovernanceStatusDeprecated).Error; err != nil {
			t.Fatal(err)
		}
		if result, err := get(); err != nil || result.State != "accepted" {
			t.Fatalf("settled deprecated history lost: %+v %v", result, err)
		}
		state = "pending"
		if _, err := get(); !errors.Is(err, ErrReferenceValidationUnavailable) {
			t.Fatalf("settled history became missing: %v", err)
		}
		state = "accepted"
		expires := time.Now().UTC().Add(40 * time.Millisecond)
		auth.Authorization.RoleAssignments[0].ValidUntil = &expires
		before = func() { waitSharingTestExpiry(t, db, expires) }
		if _, err := get(); !errors.Is(err, ErrSharingHandlingForbidden) {
			t.Fatalf("permission expired during resolve: %v", err)
		}
		before = nil
	})
}
