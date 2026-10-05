package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auth "github.com/addp/common/authorization"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestObservabilityIdentityRouteRequiresOnlyMonitorPlatformService(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&models.HostNode{}, &models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}); err != nil {
		t.Fatal(err)
	}
	actor := testIAMServiceActorContext("platform", "addp-monitor")
	resolver := &iamActorResolver{authContext: &actor}
	authentication, err := middleware.NewIAMAuthenticationMiddleware(resolver)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &IAMRuntime{Authentication: authentication, ServiceCredential: credential}
	h := NewModuleRegistryHandler(service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db)), nil)
	router := gin.New()
	if err := RegisterObservabilityIdentityRoute(router.Group("/api/v1/system"), runtime, h); err != nil {
		t.Fatal(err)
	}
	grant := func() {
		actor.Authorization.RoleAssignments = []auth.RoleAssignment{{AssignmentID: "901", RoleKey: "platform.monitor_runtime", Scope: auth.AssignmentScope{Type: actor.Context.Type, TenantID: actor.Context.TenantID}, Permissions: []string{"system.observability_identity.read"}, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt}}
	}
	request := func(query, body, token string, ctx context.Context, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/observability-identities"+query, strings.NewReader(body)).WithContext(ctx)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
		}
		return w
	}
	ctx := context.Background()
	grant()
	w := request("", "", "addp_at_test", ctx, 200)
	var snapshot commonmodels.ObservabilityIdentitySnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || snapshot.Nodes == nil || snapshot.ModuleInstances == nil || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response=%s %v", w.Body.String(), err)
	}
	request("", "", "", ctx, 401)
	for _, client := range []string{"addp-gateway", "addp-log-observer", "addp-system"} {
		actor = testIAMServiceActorContext("platform", client)
		grant()
		request("", "", "addp_at_test", ctx, 403)
	}
	actor = testIAMActorContext("platform")
	grant()
	request("", "", "addp_at_test", ctx, 403)
	actor = testIAMServiceActorContext("tenant", "addp-monitor")
	grant()
	request("", "", "addp_at_test", ctx, 403)
	actor = testIAMActorContext("tenant")
	actor.Token.Type = middleware.IAMTokenTypeDelegatedAccess
	clientID := "addp-monitor"
	actor.Client.ClientID = &clientID
	actor.Client.ScopeMode = "restricted"
	actor.Client.Audiences = []string{"system"}
	actor.Client.Scopes = []string{"workflow.run"}
	actor.Delegation = &auth.DelegationFacts{DelegatedByClientID: clientID, AgentRunID: "test-run", ToolCallID: "test-call"}
	grant()
	request("", "", "addp_at_test", ctx, 403)
	actor = testIAMServiceActorContext("platform", "addp-monitor")
	request("", "", "addp_at_test", ctx, 403)
	grant()
	for _, query := range []string{"?page=1", "?node_id=x", "?%ZZ", "?=", "?label=x&label=y"} {
		request(query, "", "addp_at_test", ctx, 400)
	}
	request("", "{}", "addp_at_test", ctx, 400)
	deadline, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	w = request("", "", "addp_at_test", deadline, 504)
	if !strings.Contains(w.Body.String(), "observability_identity_timeout") {
		t.Fatal(w.Body.String())
	}
	nodes := make([]models.HostNode, commonmodels.ObservabilityIdentityNodeLimit+1)
	for i := range nodes {
		nodes[i] = models.HostNode{NodeID: uuid.NewString(), DisplayName: "node", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{}}
	}
	if err := db.CreateInBatches(nodes, 100).Error; err != nil {
		t.Fatal(err)
	}
	w = request("", "", "addp_at_test", ctx, 503)
	if !strings.Contains(w.Body.String(), "observability_identity_budget_exceeded") || strings.Contains(w.Body.String(), "\"nodes\"") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	if err := db.Migrator().DropTable(&models.HostNode{}); err != nil {
		t.Fatal(err)
	}
	w = request("", "", "addp_at_test", ctx, 503)
	if !strings.Contains(w.Body.String(), "observability_identity_unavailable") || strings.Contains(w.Body.String(), "no such table") || strings.Contains(w.Body.String(), "nodes") {
		t.Fatal(w.Body.String())
	}
}
