package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type authorizationFactsReaderFunc func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error)

func (f authorizationFactsReaderFunc) ReadAuthorizationFacts(ctx context.Context, tenant uint, fps []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
	result := &authorizationfacts.AuthorizationFactsBatch{}
	for _, fp := range fps {
		facts, err := f(ctx, tenant, fp)
		if status, ok := client.TenantAPIStatusCode(err); ok && status == http.StatusNotFound {
			facts, err = nil, nil
		} else if facts == nil && err == nil {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		result.Data = append(result.Data, authorizationfacts.AuthorizationFactsResult{ItemFingerprint: fp, Facts: facts})
	}
	return result, nil
}

func exerciseAuthorizationFactsAPI(t *testing.T, db *gorm.DB, identity *iam.Repository, actor engineaccess.Actor, engine models.Engine) {
	t.Run("qualified authorization facts observation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: actor.TenantID, RoleKey: "custom.authorization_fact_reader", Name: "Authorization facts reader", PermissionKeys: []string{"system.engine_access_grant.read", "system.engine_access_delegation.create", "meta.scan_task.execute"}, ActorPrincipalID: actor.PrincipalID})
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
				Ancestors: []authorizationfacts.IdentityNode{{ID: 1, Name: "", NodeType: "server"}, {ID: 2, Name: "outdoor", NodeType: "schema"}},
			}},
			Total: 1, Page: 1, PageSize: 100, TotalPages: 1,
		}
		expectedQuery := authorizationfacts.IdentityQuery{EngineID: engine.ID, Page: 1, PageSize: 100}
		var calls atomic.Int32
		var unavailable atomic.Bool
		var nodeCalls atomic.Int32
		nodeTarget := plugin.FileDirectoryPath(engine.ID, "images")
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
			case "/api/v1/meta/runtime/node-identities/resolve":
				nodeCalls.Add(1)
				var query authorizationfacts.NodeIdentityQuery
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer addp_at_identity_fixture" ||
					r.Header.Get("X-Tenant-ID") != "" || json.NewDecoder(r.Body).Decode(&query) != nil || !reflect.DeepEqual(query.Target, nodeTarget) {
					t.Error("wrong node identity owner query or forwarded human credential")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if unavailable.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":"unavailable","error_code":"fixture_unavailable"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(authorizationfacts.NodeIdentity{TenantID: uint(actor.TenantID), Target: nodeTarget, Exists: true})
			case "/api/v1/meta/runtime/data-item-identities/query":
				calls.Add(1)
				var query authorizationfacts.IdentityQuery
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer addp_at_identity_fixture" ||
					r.Header.Get("X-Tenant-ID") != "" || json.NewDecoder(r.Body).Decode(&query) != nil ||
					!reflect.DeepEqual(query, expectedQuery) {
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
			path := fmt.Sprintf("/api/v1/system/engines/%d/data-items?page=1&page_size=100", engine.ID)
			if expectedQuery.Parent != nil {
				raw, _ := json.Marshal(expectedQuery.Parent)
				path += "&parent=" + url.QueryEscape(string(raw)) + "&search=" + url.QueryEscape(expectedQuery.Search)
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
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
		parent := plugin.TabularNamespacePath(engine.ID, "schema", "outdoor")
		expectedQuery.Parent, expectedQuery.Search = &parent, "persons"
		response = request()
		if response.Code != http.StatusOK || calls.Load() != 2 {
			t.Fatalf("scoped production identity query: status=%d calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
		}
		unavailable.Store(true)
		response = request()
		var failure map[string]any
		if response.Code != http.StatusServiceUnavailable || json.Unmarshal(response.Body.Bytes(), &failure) != nil ||
			failure["error_code"] != "authorization_facts_unavailable" || len(failure) != 2 || calls.Load() != 3 {
			t.Fatalf("owner failure must not become empty inventory or retry: status=%d calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
		}
		// Use the same production router and real User session as the page.
		// A handler/service-only fixture would hide a missing node reader.
		t.Run("NFS node refresh preserves current Grant and owner failure boundaries", func(t *testing.T) {
			if err := db.Exec("UPDATE system.engines SET engine_type='nfs' WHERE id=?", engine.ID).Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Exec("UPDATE system.engines SET engine_type=? WHERE id=?", engine.EngineType, engine.ID).Error; err != nil {
					t.Error(err)
				}
			}()
			id := uuid.New()
			raw, _ := json.Marshal(nodeTarget)
			if err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,node_recursive,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,reason)
VALUES(?,'independent',?,?,?::jsonb,'','',false,'user',?,'metadata_read','until_revoked',?,?,?,'Router refresh fixture')`, id, actor.TenantID, engine.ID, string(raw), actor.PrincipalID, actor.PrincipalID, actor.MembershipID, actor.AuthorizationVersion).Error; err != nil {
				t.Fatal(err)
			}
			query := shared.MetadataRefreshQuery{Kind: "node", Target: nodeTarget}
			body, _ := json.Marshal(query)
			refreshRequest := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/system/engine-access/metadata-refresh-scopes/query", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+session.Session.AccessToken)
				r := httptest.NewRecorder()
				router.ServeHTTP(r, req)
				return r
			}
			unavailable.Store(false)
			result := refreshRequest()
			var scope shared.MetadataRefreshScope
			if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &scope) != nil || scope.Validate() != nil || !reflect.DeepEqual(scope.Query, query) || scope.TenantID != actor.TenantID || scope.Operator.PrincipalID != actor.PrincipalID || len(scope.Ranges) != 1 || scope.Ranges[0].Node == nil || scope.Ranges[0].Node.Recursive || !reflect.DeepEqual(scope.Ranges[0].Node.Target, nodeTarget) || nodeCalls.Load() != 2 || result.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("production NFS refresh: status=%d node_calls=%d body=%s", result.Code, nodeCalls.Load(), result.Body.String())
			}
			unavailable.Store(true)
			result = refreshRequest()
			var failure map[string]any
			if result.Code != 503 || json.Unmarshal(result.Body.Bytes(), &failure) != nil || failure["error_code"] != "authorization_facts_unavailable" || len(failure) != 2 || nodeCalls.Load() != 3 {
				t.Fatalf("node owner failure retried or admitted: status=%d calls=%d body=%s", result.Code, nodeCalls.Load(), result.Body.String())
			}
			unavailable.Store(false)
			if err := db.Exec(`INSERT INTO system.engine_access_grant_revocations(request_id,revoked_by_principal_id,revoked_by_membership_id,reason,revoked_request_ids) VALUES(?,?,?,'Router fixture withdrawal',jsonb_build_array((?::uuid)::text))`, id, actor.PrincipalID, actor.MembershipID, id).Error; err != nil {
				t.Fatal(err)
			}
			result = refreshRequest()
			if result.Code != 403 {
				t.Fatalf("withdrawn node still refreshes: status=%d body=%s", result.Code, result.Body.String())
			}
		})
	})
}
