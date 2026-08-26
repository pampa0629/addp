package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
)

func TestPrepareWorkflowExecutionAuthorizationAggregatesEffectsAndEngines(t *testing.T) {
	var captured commonClient.IssueExecutionAuthorizationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/auth/execution-authorizations" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer addp_at_user" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("legacy internal headers must not be sent: %#v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
			ID: "91", ExecutionID: captured.ExecutionID, Audience: captured.Audience,
			EngineIDs: captured.EngineIDs, Effects: captured.Effects,
			ExpiresAt: time.Now().Add(10 * time.Minute), ActorPrincipalID: "11", TenantID: "7",
			TenantMembershipID: "13", IssuedAuthorizationVersion: "17",
		})
	}))
	defer server.Close()

	tenantID := uint(7)
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities("geopython_workflow", plugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON := commonModels.JSONString(capabilities)
	discovery := &OperatorDiscoveryService{
		getRuntimeDescriptor: func(_ context.Context, gotTenantID, _ uint) (*commonModels.EngineRuntimeDescriptor, error) {
			if gotTenantID != tenantID {
				t.Fatalf("tenant id = %d, want %d", gotTenantID, tenantID)
			}
			return &commonModels.EngineRuntimeDescriptor{
				ID: 50, Name: "workflow", EngineType: "geopython_workflow",
				LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline, Capabilities: &capabilitiesJSON,
				RuntimeEndpoint: &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: "workflow", Port: 8099},
			}, nil
		},
		listWorkflowOperators: func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
			return []commonModels.OperatorDescriptor{
				{
					ID: "load", Name: "load", EngineType: "geopython_workflow",
					ExecutionModes: []string{"workflow"}, Effects: []string{"read"},
					Parameters: []commonModels.ParameterDescriptor{
						{Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"},
					},
				},
				{
					ID: "save", Name: "save", EngineType: "geopython_workflow",
					ExecutionModes: []string{"workflow"}, Effects: []string{"write"},
					Parameters: []commonModels.ParameterDescriptor{
						{Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"}, {Name: "mode"},
					},
				},
			}, nil
		},
	}
	executor := &DevExecutor{
		operatorDiscovery: discovery,
		sqlEngine: NewSQLEngineService(
			&config.Config{}, nil, commonClient.NewSystemExecutionAuthorizationClient(server.URL, server.Client()),
		),
	}
	executionID := uuid.New().String()
	authorization, err := executor.prepareWorkflowExecutionAuthorization(
		context.Background(),
		&models.DevTask{
			DevType: "workflow", Timeout: 300,
			Content: models.DevTaskContent{"workflow_definition": map[string]interface{}{
				"tasks": []interface{}{
					map[string]interface{}{
						"id": "load", "operator": "load", "depends_on": []interface{}{},
						"params": map[string]interface{}{"locator": "addp://engine/12/path/public/source?type=table"},
					},
					map[string]interface{}{
						"id": "save", "operator": "save", "depends_on": []interface{}{"load"},
						"params": map[string]interface{}{
							"input_df":              map[string]interface{}{"$ref": "load"},
							"target_parent_locator": "addp://engine/13/path/analytics?type=schema",
							"target_name":           "result",
						},
					},
				},
			}},
			ExecutionConfig: models.DevTaskContent{"engine_id": float64(50)},
		},
		7, "addp_at_user", executionID,
	)
	if err != nil {
		t.Fatalf("prepareWorkflowExecutionAuthorization() error = %v", err)
	}
	if authorization.AuthorizationID != 91 || authorization.ActorPrincipalID != 11 ||
		authorization.ActorTenantMembershipID != 13 || authorization.IssuedAuthorizationVersion != 17 {
		t.Fatalf("authorization facts = %#v", authorization)
	}
	if !reflect.DeepEqual(captured.Effects, []string{"read", "write"}) {
		t.Fatalf("effects = %#v", captured.Effects)
	}
	engineIDs := append([]string(nil), captured.EngineIDs...)
	sort.Strings(engineIDs)
	if !reflect.DeepEqual(engineIDs, []string{"12", "13", "50"}) {
		t.Fatalf("engine_ids = %#v", captured.EngineIDs)
	}
	if !reflect.DeepEqual(authorization.EngineEffects[12], []string{"read"}) ||
		!reflect.DeepEqual(authorization.EngineEffects[13], []string{"write"}) ||
		!reflect.DeepEqual(authorization.EngineEffects[50], []string{"read", "write"}) {
		t.Fatalf("engine effects = %#v", authorization.EngineEffects)
	}
}

func TestPrepareWorkflowExecutionAuthorizationRequiresUserToken(t *testing.T) {
	executor := &DevExecutor{}
	_, err := executor.prepareWorkflowExecutionAuthorization(
		context.Background(), &models.DevTask{DevType: "workflow"}, 7, "", uuid.New().String(),
	)
	if err == nil {
		t.Fatal("prepareWorkflowExecutionAuthorization() error = nil, want User token requirement")
	}
}

func TestIssueManagedWriteExecutionAuthorizationFromExecutionRequestsReadAndWrite(t *testing.T) {
	var captured commonClient.IssueExecutionAuthorizationFromExecutionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/system/runtime/execution-authorizations" {
			http.NotFound(w, request)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
			ID: "91", ExecutionID: captured.ExecutionID, Audience: captured.Audience,
			EngineIDs: captured.EngineIDs, Effects: captured.Effects,
			ExpiresAt: time.Now().Add(10 * time.Minute), ActorPrincipalID: "11", TenantID: "7",
			TenantMembershipID: "13", IssuedAuthorizationVersion: "17",
		})
	}))
	defer server.Close()

	systemClient := commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_develop"), server.Client())
	authorization, err := NewSQLEngineService(&config.Config{}, systemClient, nil).IssueManagedWriteExecutionAuthorizationFromExecution(
		context.Background(), 7, uuid.New(), uuid.New(), 12, 60,
	)
	if err != nil {
		t.Fatalf("IssueManagedWriteExecutionAuthorizationFromExecution() error = %v", err)
	}
	if !reflect.DeepEqual(captured.Effects, []string{"read", "write"}) ||
		!reflect.DeepEqual(authorization.Effects, []SQLExecutionEffect{SQLExecutionEffectRead, SQLExecutionEffectWrite}) {
		t.Fatalf("request effects=%#v authorization=%#v", captured.Effects, authorization.Effects)
	}
}
