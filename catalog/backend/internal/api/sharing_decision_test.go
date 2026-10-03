package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	"github.com/addp/catalog/internal/service"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type sharingRouteTarget struct{}

func (sharingRouteTarget) ResolveSharingTarget(context.Context, int64, int64, string) (plugin.EngineCatalogPath, error) {
	return plugin.EngineCatalogBranchLeafPath(plugin.TabularCatalogModel("schema"), 12, "schema", "public", "table", "table", "orders"), nil
}

type sharingRouteReferences struct{}

func TestSharingDecisionSwaggerRequiresModeAndAllowsExplicitNullDate(t *testing.T) {
	data, err := os.ReadFile("../../docs/swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Definitions map[string]struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Format   string   `json:"format"`
				Nullable bool     `json:"x-nullable"`
				Enum     []string `json:"enum"`
			} `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"internal_api.createSharingDecisionRequest", "github_com_addp_catalog_internal_service.SharingDecisionResult"} {
		definition, ok := document.Definitions[name]
		date, mode := definition.Properties["expires_at"], definition.Properties["expiry_mode"]
		if !ok || !date.Nullable || date.Format != "date-time" || len(mode.Enum) != 2 || mode.Enum[0] != "at_time" || mode.Enum[1] != "until_revoked" {
			t.Fatalf("expiry contract missing in %s: %+v", name, definition)
		}
		if name == "internal_api.createSharingDecisionRequest" {
			required := false
			for _, field := range definition.Required {
				if field == "expires_at" {
					t.Fatal("date must be conditional on the explicit mode")
				}
				required = required || field == "expiry_mode"
			}
			if !required {
				t.Fatal("expiry mode must not be optional")
			}
		}
	}
}

func (sharingRouteReferences) ResolveSystemReferences(_ context.Context, _ int64, refs []commonClient.SystemCatalogReference) ([]commonClient.SystemCatalogReferenceResolution, error) {
	result := make([]commonClient.SystemCatalogReferenceResolution, len(refs))
	for i, ref := range refs {
		result[i] = commonClient.SystemCatalogReferenceResolution{SubjectType: ref.SubjectType, ID: ref.ID, Found: true, Referenceable: true}
	}
	return result, nil
}

func TestPostgresSharingDecisionRoutesUseExplicitPermissionAndUserIdentity(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable Catalog PostgreSQL gate")
	}
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	db = db.Begin()
	if db.Error != nil {
		t.Fatal(db.Error)
	}
	defer db.Rollback()
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	id, decisionID := uuid.New(), uuid.New()
	if err := db.Create(&models.Entry{ID: id, TenantID: 7, EntryType: models.EntryTypeDataItem, EntryStatus: models.EntryStatusActive, GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SourceBinding{ID: uuid.New(), TenantID: 7, CatalogEntryID: id, SourceModule: models.SourceModuleMeta, SourceType: models.SourceTypeDataItem, SourceIdentity: uuid.NewString(), SourceVersion: "00000000000000000001", IsCurrent: true, SourceStatus: models.SourceStatusActive, ObservedSnapshot: commonModels.JSONMap{"item_id": 21}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: id, Role: models.ResponsibilityRoleBusinessOwner, SubjectType: "user", SubjectID: 40, Status: models.ResponsibilityStatusActive, ObservedSnapshot: commonModels.JSONMap{}, VerifiedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	permissions := []string{"catalog.entry.read"}
	tenant, user := "7", "40"
	scopeStatus, scopeCalls := http.StatusOK, 0
	bearerHeader := "Bearer addp_at_test"
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer addp_at_test" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("human handling substituted credentials or tenant headers")
		}
		w.Header().Set("Content-Type", "application/json")
		auth := authtest.NewTenantUserAuthContext(tenant, user, permissions)
		switch r.URL.Path {
		case "/api/v1/system/auth/context":
			_ = json.NewEncoder(w).Encode(auth)
		case "/api/v1/system/engines/12/access_handling_scope":
			scopeCalls++
			w.WriteHeader(scopeStatus)
			p, _ := strconv.ParseInt(auth.Principal.ID, 10, 64)
			m, _ := strconv.ParseInt(*auth.Context.TenantMembershipID, 10, 64)
			v, _ := strconv.ParseInt(auth.Authorization.AuthorizationVersion, 10, 64)
			_ = json.NewEncoder(w).Encode(authorization.EngineAccessHandlingScope{TenantID: 7, EngineID: 12,
				Operator: authorization.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}, VerifiedAt: time.Now().UTC()})
		default:
			t.Errorf("unexpected System request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer system.Close()
	entries := service.NewEntryService(db, nil, sharingRouteReferences{}).WithSharingTargetResolver(sharingRouteTarget{}).
		WithSharingHandlingScopeReader(commonClient.NewSystemServiceClient(system.URL, nil, system.Client()))
	router := SetupRouter(system.URL, modulelifecycle.NewStandalone("catalog"), entries, nil, nil, nil, nil, nil)
	path := "/api/v1/catalog/entries/" + id.String() + "/sharing_decisions"
	body := fmt.Sprintf(`{"decision_id":%q,"version":"1","recipient_type":"user","recipient_id":"40","expiry_mode":"at_time","expires_at":%q,"reason":"Explicit self read"}`, decisionID.String(), time.Now().UTC().AddDate(8, 0, 0).Format(time.RFC3339Nano))
	request := func(method, url, data string, want int) string {
		t.Helper()
		req := httptest.NewRequest(method, url, strings.NewReader(data))
		req.Header.Set("Authorization", bearerHeader)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, url, response.Code, want, response.Body.String())
		}
		return response.Body.String()
	}
	request(http.MethodPost, path, body, http.StatusForbidden)
	permissions = append(permissions, "catalog.entry.update")
	request(http.MethodPost, path, body, http.StatusForbidden)
	permissions = []string{"catalog.entry.read", "catalog.inventory.read", "catalog.sharing_decision.create"}
	for _, invalid := range []string{
		strings.Replace(body, `"expiry_mode":"at_time",`, "", 1),
		strings.Replace(body, `"expiry_mode":"at_time"`, `"expiry_mode":"until_revoked"`, 1),
		strings.Replace(body, `"expiry_mode":"at_time"`, `"expiry_mode":"unknown"`, 1),
	} {
		request(http.MethodPost, path, invalid, http.StatusBadRequest)
	}
	for _, invalid := range []string{strings.Replace(body, `"version":"1"`, `"version":1`, 1), strings.Replace(body, `"recipient_id":"40"`, `"recipient_id":"040"`, 1), strings.TrimSuffix(body, "}") + `,"confirmed_by":"40"}`, strings.TrimSuffix(body, "}") + `,"action":"read"}`} {
		request(http.MethodPost, path, invalid, http.StatusBadRequest)
	}
	user = "41"
	request(http.MethodPost, path, body, http.StatusForbidden)
	user = "40"
	request(http.MethodPost, path, body, http.StatusCreated)
	request(http.MethodPost, path, body, http.StatusOK)
	request(http.MethodPost, path, strings.Replace(body, "Explicit self read", "Changed", 1), http.StatusConflict)
	request(http.MethodGet, path+"/"+decisionID.String(), "", http.StatusOK)
	longID := uuid.New()
	longBody := fmt.Sprintf(`{"decision_id":%q,"version":"2","recipient_type":"user","recipient_id":"40","expiry_mode":"until_revoked","reason":"Explicit long-term read"}`, longID.String())
	longResult := request(http.MethodPost, path, longBody, http.StatusCreated)
	var longView map[string]any
	if err := json.Unmarshal([]byte(longResult), &longView); err != nil || longView["expiry_mode"] != "until_revoked" || longView["expires_at"] != nil {
		t.Fatalf("long-term expiry response=%s err=%v", longResult, err)
	}
	request(http.MethodPost, path, strings.TrimSuffix(longBody, "}")+`,"expires_at":null}`, http.StatusOK)
	request(http.MethodGet, path+"/"+longID.String(), "", http.StatusOK)
	user = "41"
	request(http.MethodGet, path+"/"+decisionID.String(), "", http.StatusNotFound)
	tenant = "8"
	request(http.MethodGet, path+"/"+decisionID.String(), "", http.StatusNotFound)
	tenant, user = "7", "40"
	permissions = []string{"catalog.entry.read"}
	request(http.MethodGet, path+"/"+decisionID.String(), "", http.StatusForbidden)
	candidatesPath := "/api/v1/catalog/entries/" + id.String() + "/sharing_decision_candidates"
	request(http.MethodGet, candidatesPath, "", http.StatusForbidden)
	permissions = []string{"catalog.entry.read", "catalog.inventory.read", "system.engine_access_fulfillment.create"}
	user = "41"
	data := request(http.MethodGet, candidatesPath+"?page=1&page_size=1", "", http.StatusOK)
	var summaries struct {
		Data  []service.SharingDecisionCandidate `json:"data"`
		Total int64                              `json:"total"`
	}
	if err := json.Unmarshal([]byte(data), &summaries); err != nil || summaries.Total != 2 || len(summaries.Data) != 1 || summaries.Data[0].ConfirmedBy != 40 || strings.Contains(data, `"reason"`) || scopeCalls != 1 {
		t.Fatalf("candidate response=%s calls=%d err=%v", data, scopeCalls, err)
	}
	bearerHeader = "bearer\taddp_at_test"
	request(http.MethodGet, candidatesPath, "", http.StatusOK)
	bearerHeader = "Bearer addp_at_test"
	request(http.MethodGet, path+"/"+decisionID.String(), "", http.StatusForbidden)
	request(http.MethodPost, path, body, http.StatusForbidden)
	scopeStatus = http.StatusForbidden
	request(http.MethodGet, candidatesPath, "", http.StatusForbidden)
	scopeStatus = http.StatusInternalServerError
	request(http.MethodGet, candidatesPath, "", http.StatusServiceUnavailable)
	tenant = "8"
	beforeCalls := scopeCalls
	request(http.MethodGet, candidatesPath, "", http.StatusNotFound)
	if scopeCalls != beforeCalls {
		t.Fatal("cross-tenant candidate queried handling scope")
	}
	tenant = "7"
	preparePath := "/api/v1/catalog/entries/" + id.String() + "/sharing_fulfillments"
	prepareBody := fmt.Sprintf(`{"request_id":%q,"decision_id":%q,"requirement_version":"1"}`, uuid.NewString(), longID.String())
	permissions = []string{"catalog.entry.read", "catalog.entry.update"}
	request(http.MethodPost, preparePath, prepareBody, http.StatusForbidden)
	permissions = []string{"catalog.entry.read", "system.engine_access_fulfillment.create"}
	for _, invalid := range []string{`{}`, strings.TrimSuffix(prepareBody, "}") + `,"operator":{"principal_id":"40"}}`, strings.Replace(prepareBody, `"requirement_version":"1"`, `"requirement_version":1`, 1)} {
		request(http.MethodPost, preparePath, invalid, http.StatusBadRequest)
	}
	request(http.MethodPost, preparePath, prepareBody, http.StatusServiceUnavailable)
}
