package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
			Accesses:  captured.Accesses,
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
	wantAccesses := []commonClient.ExecutionEngineAccessScope{
		{EngineID: "12", Effects: []string{"read"}},
		{EngineID: "13", Effects: []string{"write"}},
		{EngineID: "50", Effects: []string{"read", "write"}},
	}
	if !reflect.DeepEqual(captured.Accesses, wantAccesses) {
		t.Fatalf("accesses = %#v, want %#v", captured.Accesses, wantAccesses)
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

func TestRasterAuthorizationSeparatesSourceReadAndTargetWrite(t *testing.T) {
	discovery := newWorkflowValidationTestService(t)
	getDescriptor := discovery.getRuntimeDescriptor
	discovery.getRuntimeDescriptor = func(ctx context.Context, tenantID, engineID uint) (*commonModels.EngineRuntimeDescriptor, error) {
		descriptor, err := getDescriptor(ctx, tenantID, engineID)
		descriptor.EngineType = "geopython_workflow"
		return descriptor, err
	}
	discovery.listWorkflowOperators = func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
		return []commonModels.OperatorDescriptor{
			{ID: "raster_load", Name: "raster_load", EngineType: "geopython_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"read"}, Parameters: []commonModels.ParameterDescriptor{{Name: "access_plan", Type: "object"}}},
			{ID: "raster_save", Name: "raster_save", EngineType: "geopython_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"write"}, Parameters: []commonModels.ParameterDescriptor{{Name: "access_plan", Type: "object"}, {Name: "input_raster", Type: "raster", ParamType: "input"}}},
		}, nil
	}
	executor := &DevExecutor{operatorDiscovery: discovery}
	plan, err := executor.buildWorkflowExecutionAuthorizationPlan(context.Background(), &models.DevTask{
		DevType: "workflow", ExecutionConfig: models.DevTaskContent{"engine_id": float64(12)},
		Content: models.DevTaskContent{"workflow_definition": map[string]interface{}{"tasks": []interface{}{
			map[string]interface{}{"id": "load", "operator": "raster_load", "params": map[string]interface{}{"locator": "addp://engine/1/path/input.tif?type=file"}},
			map[string]interface{}{"id": "save", "operator": "raster_save", "params": map[string]interface{}{"target_parent_locator": "addp://engine/2/path/business?type=bucket", "target_name": "result.tif"}},
		}}},
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint][]string{1: {"read"}, 2: {"write"}, 12: {"read", "write"}}
	if !reflect.DeepEqual(plan.engineEffects, want) {
		t.Fatalf("effects = %#v, want %#v", plan.engineEffects, want)
	}
}

func TestSparkSaveDDLBelongsOnlyToTableTargetAndRuntime(t *testing.T) {
	for _, parentType := range []string{"schema", "database", "bucket", "prefix"} {
		t.Run(parentType, func(t *testing.T) {
			discovery := newWorkflowValidationTestService(t)
			getDescriptor := discovery.getRuntimeDescriptor
			discovery.getRuntimeDescriptor = func(ctx context.Context, tenantID, engineID uint) (*commonModels.EngineRuntimeDescriptor, error) {
				descriptor, err := getDescriptor(ctx, tenantID, engineID)
				descriptor.ID = engineID
				descriptor.EngineType = "spark_workflow"
				return descriptor, err
			}
			discovery.listWorkflowOperators = func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
				return []commonModels.OperatorDescriptor{
					{ID: "load", Name: "load", EngineType: "spark_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"read"}, Parameters: []commonModels.ParameterDescriptor{{Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"}, {Name: "format"}, {Name: "index"}}},
					{ID: "save", Name: "save", EngineType: "spark_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"write", "ddl"}, Parameters: []commonModels.ParameterDescriptor{{Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"}, {Name: "mode"}}},
				}, nil
			}
			executor := &DevExecutor{operatorDiscovery: discovery}
			plan, err := executor.buildWorkflowExecutionAuthorizationPlan(context.Background(), &models.DevTask{
				DevType: "workflow", ExecutionConfig: models.DevTaskContent{"engine_id": float64(50), "engine_specific": map[string]interface{}{"spark_cluster_id": float64(51)}},
				Content: models.DevTaskContent{"workflow_definition": map[string]interface{}{"tasks": []interface{}{
					map[string]interface{}{"id": "load", "operator": "load", "params": map[string]interface{}{"locator": "addp://engine/1/path/orders.parquet?type=file"}},
					map[string]interface{}{"id": "save", "operator": "save", "params": map[string]interface{}{"target_parent_locator": "addp://engine/2/path/results?type=" + parentType, "target_name": "totals"}},
				}}},
			}, 7)
			if err != nil {
				t.Fatal(err)
			}
			targetEffects := []string{"write"}
			if parentType == "schema" || parentType == "database" {
				targetEffects = append(targetEffects, "ddl")
			}
			want := map[uint][]string{1: {"read"}, 2: targetEffects, 50: {"read", "write", "ddl"}, 51: {"read"}}
			if !reflect.DeepEqual(plan.engineEffects, want) {
				t.Fatalf("effects = %#v, want %#v", plan.engineEffects, want)
			}
		})
	}
}
