package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type authorizationFactsReaderFunc func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error)

func (f authorizationFactsReaderFunc) ReadAuthorizationFacts(ctx context.Context, tenant uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
	return f(ctx, tenant, fingerprint)
}

func exerciseAuthorizationFactsAPI(t *testing.T, db *gorm.DB, identity *iam.Repository, actor engineaccess.Actor, engine models.Engine) {
	t.Run("qualified authorization facts observation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: actor.TenantID, RoleKey: "custom.authorization_fact_reader", Name: "Authorization facts reader", PermissionKeys: []string{"system.engine_access_grant.read", "system.engine_access_delegation.create"}, ActorPrincipalID: actor.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		stepUp := time.Now().Add(time.Minute)
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: actor.TenantID, MembershipID: actor.MembershipID, RoleIDs: []int64{role.ID}, Reason: "Read exact resource facts", ActorPrincipalID: actor.PrincipalID, AssuranceLevel: iam.AssuranceLevelAAL2, StepUpExpiresAt: &stepUp}); err != nil {
			t.Fatal(err)
		}
		refresh := func() {
			p, err := identity.GetPrincipal(ctx, actor.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			actor.AuthorizationVersion = p.AuthorizationVersion
		}
		refresh()
		exerciseAuthorizationItemsRouter(t, db, actor, engine)
		actor.TokenExpiresAt = time.Now().Add(time.Minute)
		fingerprint := commonmodels.GenerateItemFingerprint(engine.ID, "outdoor.persons")
		facts := &authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, ItemFingerprint: fingerprint, EngineID: engine.ID, ItemType: "table", FullName: "outdoor.persons", BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: plugin.TabularItemPath(engine.ID, "schema", "outdoor", "persons")}}
		hash, err := facts.ComputeBoundaryHash()
		if err != nil {
			t.Fatal(err)
		}
		facts.BoundaryHash = &hash
		calls := 0
		reader := authorizationFactsReaderFunc(func(_ context.Context, tenant uint, requested string) (*authorizationfacts.AuthorizationFacts, error) {
			calls++
			if int64(tenant) != actor.TenantID || requested != fingerprint {
				t.Fatal("wrong resource context")
			}
			return facts, nil
		})
		s := engineaccess.NewService(engineaccess.NewRepository(db), nil).WithAuthorizationFactsReader(reader)
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); err != nil {
			t.Fatal(err)
		}
		other := *facts
		other.EngineID++
		other.ItemFingerprint = commonmodels.GenerateItemFingerprint(other.EngineID, other.FullName)
		otherBoundary := *facts.Boundary
		otherBoundary.NativeTarget = plugin.TabularItemPath(other.EngineID, "schema", "outdoor", "persons")
		other.Boundary = &otherBoundary
		otherHash, err := other.ComputeBoundaryHash()
		if err != nil {
			t.Fatal(err)
		}
		other.BoundaryHash = &otherHash
		s.WithAuthorizationFactsReader(authorizationFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
			return &other, nil
		}))
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), other.ItemFingerprint); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal("another engine target was exposed under selected engine", err)
		}
		s.WithAuthorizationFactsReader(reader)
		bad := actor
		bad.AuthorizationVersion--
		before := calls
		if _, err := s.GetAuthorizationFacts(ctx, bad, int64(engine.ID), fingerprint); !errors.Is(err, commonapi.ErrForbidden) || calls != before {
			t.Fatal("stale actor called Meta", err)
		}
		s.WithAuthorizationFactsReader(authorizationFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
			return nil, &client.TenantAPIError{StatusCode: http.StatusNotFound}
		}))
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal("missing target confused with dependency failure", err)
		}
		s.WithAuthorizationFactsReader(authorizationFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
			// Must complete while the remote read is in flight: no local IAM lock may remain held.
			if err := db.WithContext(ctx).Exec("UPDATE system.principals SET authorization_version=authorization_version+1 WHERE id=?", actor.PrincipalID).Error; err != nil {
				return nil, err
			}
			return facts, nil
		}))
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("remote read outlived changed human authority", err)
		}
		refresh()
		s.WithAuthorizationFactsReader(nil)
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); !errors.Is(err, engineaccess.ErrAuthorizationFactsUnavailable) {
			t.Fatal("missing service credential silently bypassed", err)
		}
	})
}

// Exercise the production composition, including the human credential guard and
// System's tenant-scoped machine client. Handler-only tests cannot detect a
// missing Meta identity reader in SetupRouter.
func exerciseAuthorizationItemsRouter(t *testing.T, db *gorm.DB, actor engineaccess.Actor, engine models.Engine) {
	t.Run("production router lists Meta identities", func(t *testing.T) {
		const machineSecret = "identity-fixture-0123456789abcdef0123456789abcdef"
		page := authorizationfacts.IdentityPage{
			Data: []authorizationfacts.DataItemIdentity{{
				EngineID: engine.ID, ItemFingerprint: commonmodels.GenerateItemFingerprint(engine.ID, "outdoor.persons"),
				Name: "persons", FullName: "outdoor.persons", ItemType: "table",
				Ancestors: []authorizationfacts.IdentityNode{{ID: 1, Name: "outdoor", NodeType: "schema"}},
			}},
			Total: 1, Page: 1, PageSize: 100, TotalPages: 1,
		}
		var calls atomic.Int32
		var unavailable atomic.Bool
		owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/system/oauth/token":
				id, secret, ok := r.BasicAuth()
				if r.Method != http.MethodPost || !ok || id != "addp-system" || secret != machineSecret ||
					r.ParseForm() != nil || r.Form.Get("grant_type") != "client_credentials" ||
					r.Form.Get("context_type") != "" || r.Form.Get("tenant_id") != strconv.FormatInt(actor.TenantID, 10) {
					t.Error("invalid System machine credential context")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"addp_at_identity_fixture","token_type":"Bearer","expires_in":300,"scope":"addp.api"}`))
			case "/api/v1/meta/runtime/data-item-identities/query":
				calls.Add(1)
				var query authorizationfacts.IdentityQuery
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer addp_at_identity_fixture" ||
					r.Header.Get("X-Tenant-ID") != "" || json.NewDecoder(r.Body).Decode(&query) != nil ||
					!reflect.DeepEqual(query, authorizationfacts.IdentityQuery{EngineID: engine.ID, Page: 1, PageSize: 100}) {
					t.Error("wrong Meta owner query or forwarded human credential")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if unavailable.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":"unavailable","error_code":"fixture_unavailable"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(page)
			default:
				t.Errorf("unexpected owner request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer owner.Close()
		cfg := testIAMRuntimeConfig()
		cfg.SystemServiceURL, cfg.MetaServiceURL = owner.URL, owner.URL
		cfg.ServiceClientSecrets = map[string]string{"addp-system": machineSecret}
		router := SetupRouter(db, cfg)
		runtime, err := NewIAMRuntime(db, cfg, testIAMSecurityPolicy())
		if err != nil {
			t.Fatal(err)
		}
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		stepUp := now.Add(time.Minute)
		session, err := runtime.ContextSelectionService.BeginContextSelection(context.Background(), iam.BeginContextSelectionInput{
			PrincipalID:    actor.PrincipalID,
			Authentication: iam.SessionAuthentication{Methods: []string{"password", "totp"}, AssuranceLevel: iam.AssuranceLevelAAL2, AuthenticatedAt: now, StepUpExpiresAt: &stepUp},
		})
		if err != nil || session == nil || session.Session == nil {
			t.Fatalf("issue fixture browser session: %v", err)
		}
		request := func() *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d/data-items?page=1&page_size=100", engine.ID), nil)
			req.Header.Set("Authorization", "Bearer "+session.Session.AccessToken)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			return response
		}
		response := request()
		var got authorizationfacts.IdentityPage
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &got) != nil || !reflect.DeepEqual(got, page) || calls.Load() != 1 {
			t.Fatalf("production identity listing: status=%d owner_calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
		}
		unavailable.Store(true)
		response = request()
		var failure map[string]any
		if response.Code != http.StatusServiceUnavailable || json.Unmarshal(response.Body.Bytes(), &failure) != nil ||
			failure["error_code"] != "authorization_facts_unavailable" || len(failure) != 2 || calls.Load() != 2 {
			t.Fatalf("owner failure must not become empty inventory or retry: status=%d calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
		}
	})
}
