package dbbridge

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type workflowOperatorProvider struct {
	operators []plugin.OperatorDescriptor
	invoked   bool
	executed  bool
}

func (p *workflowOperatorProvider) Type() string { return "test_workflow_bridge" }

func (p *workflowOperatorProvider) DisplayName() string { return "Test Workflow Bridge" }

func (p *workflowOperatorProvider) EngineOrigin() string { return "extension" }

func (p *workflowOperatorProvider) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *workflowOperatorProvider) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return nil
}

func (p *workflowOperatorProvider) DefaultPort() int { return 0 }

func (p *workflowOperatorProvider) RequiredFields() []string { return nil }

func (p *workflowOperatorProvider) SensitiveFields() []string { return nil }

func (p *workflowOperatorProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.NewWorkflowCapabilities(p.Type(), "addp.workflow/v1")
}

func (p *workflowOperatorProvider) RuntimeEndpoint(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	return "http://localhost:1", nil
}

func (p *workflowOperatorProvider) ListOperators(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.OperatorDescriptor, error) {
	min := 1.0
	if p.operators != nil {
		return p.operators, nil
	}
	return []plugin.OperatorDescriptor{
		{
			ID:               "buffer",
			Name:             "buffer",
			DisplayName:      "缓冲区",
			EngineType:       p.Type(),
			Type:             "spatial",
			Category:         "空间分析",
			CategoryPath:     []string{"空间分析"},
			Description:      "生成缓冲区",
			BriefDescription: "围绕输入几何生成指定距离的缓冲区",
			DetailedDescription: map[string]interface{}{
				"overview": "缓冲区分析",
			},
			Parameters: []plugin.ParameterDescriptor{
				{
					Name:        "distance",
					Type:        "float",
					Required:    true,
					Description: "缓冲距离",
					Min:         &min,
					Properties: map[string]plugin.ParameterDescriptor{
						"unit": {
							Name:        "unit",
							Type:        "string",
							Description: "距离单位",
						},
					},
				},
			},
			Inputs: []interface{}{
				"geodataframe",
				map[string]interface{}{"type": "dataframe"},
			},
			OutputPorts: []plugin.OutputPortDescriptor{
				{
					Name:        "default",
					Type:        "geodataframe",
					Description: "缓冲区结果",
					IsDefault:   true,
				},
			},
			ExecutionModes: []string{"workflow"},
			Attributes: map[string]interface{}{
				"direct_binary": map[string]interface{}{
					"content_type": "application/vnd.apache.arrow.stream",
				},
			},
		},
	}, nil
}

func (p *workflowOperatorProvider) ExecuteWorkflow(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.WorkflowExecuteRequest) (*plugin.WorkflowExecuteResult, error) {
	p.executed = true
	return &plugin.WorkflowExecuteResult{Status: "success"}, nil
}

func (p *workflowOperatorProvider) InvokeOperator(ctx context.Context, connInfo plugin.ConnectionInfo, operatorName string, req plugin.OperatorInvokeRequest) (*plugin.OperatorInvokeResult, error) {
	p.invoked = true
	return &plugin.OperatorInvokeResult{Status: "success"}, nil
}

func (p *workflowOperatorProvider) GetExecutionStatus(ctx context.Context, connInfo plugin.ConnectionInfo, executionID string) (*plugin.WorkflowExecutionStatus, error) {
	return &plugin.WorkflowExecutionStatus{Status: "success", ExecutionID: executionID, Progress: 100}, nil
}

func TestListWorkflowOperatorsPreservesOperatorDescriptor(t *testing.T) {
	provider := &workflowOperatorProvider{}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	operators, err := ListWorkflowOperators(context.Background(), engine)
	if err != nil {
		t.Fatalf("ListWorkflowOperators returned error: %v", err)
	}
	if len(operators) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(operators))
	}

	op := operators[0]
	if op.ID != "buffer" || op.Name != "buffer" || op.EngineType != provider.Type() {
		t.Fatalf("unexpected operator identity: %+v", op)
	}
	if len(op.CategoryPath) != 1 || op.CategoryPath[0] != "空间分析" {
		t.Fatalf("unexpected operator category path: %+v", op.CategoryPath)
	}
	if len(op.ExecutionModes) != 1 || op.ExecutionModes[0] != "workflow" {
		t.Fatalf("unexpected operator execution modes: %+v", op.ExecutionModes)
	}
	if op.BriefDescription == "" || op.DetailedDescription["overview"] != "缓冲区分析" {
		t.Fatalf("operator descriptions were not preserved: %+v", op)
	}
	if len(op.Parameters) != 1 || op.Parameters[0].Min == nil || op.Parameters[0].Properties["unit"].Type != "string" {
		t.Fatalf("operator parameters were not preserved: %+v", op.Parameters)
	}
	if len(op.Inputs) != 2 || op.Inputs[0] != "geodataframe" || op.Inputs[1] != "dataframe" {
		t.Fatalf("operator inputs were not converted as expected: %+v", op.Inputs)
	}
	if len(op.OutputPorts) != 1 || !op.OutputPorts[0].IsDefault {
		t.Fatalf("operator output ports were not preserved: %+v", op.OutputPorts)
	}
	if op.Attributes["direct_binary"] == nil {
		t.Fatalf("operator attributes were not preserved: %+v", op.Attributes)
	}
}

func TestGetWorkflowExecutionStatusUsesWorkflowRuntimeProvider(t *testing.T) {
	provider := &workflowOperatorProvider{}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	status, err := GetWorkflowExecutionStatus(context.Background(), engine, "runtime-exec-1")
	if err != nil {
		t.Fatalf("GetWorkflowExecutionStatus() error = %v", err)
	}
	if status.Status != "success" || status.ExecutionID != "runtime-exec-1" || status.Progress != 100 {
		t.Fatalf("status = %+v, want runtime execution status", status)
	}
}

func TestListWorkflowOperatorsRejectsIncompleteOperatorMetadata(t *testing.T) {
	provider := &workflowOperatorProvider{
		operators: []plugin.OperatorDescriptor{
			{
				ID:             "buffer",
				Name:           "buffer",
				DisplayName:    "缓冲区",
				EngineType:     "test_workflow_bridge",
				Type:           "spatial",
				Category:       "空间分析",
				Description:    "生成缓冲区",
				Parameters:     []plugin.ParameterDescriptor{},
				OutputPorts:    []plugin.OutputPortDescriptor{},
				ExecutionModes: []string{"workflow"},
			},
		},
	}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	_, err := ListWorkflowOperators(context.Background(), engine)
	if err == nil {
		t.Fatal("expected incomplete operator metadata to be rejected")
	}
	if !strings.Contains(err.Error(), "category_path") {
		t.Fatalf("expected category_path validation error, got %v", err)
	}
}

func TestInvokeOperatorAllowsDirectOperator(t *testing.T) {
	provider := &workflowOperatorProvider{
		operators: []plugin.OperatorDescriptor{
			{
				ID:             "buffer",
				Name:           "buffer",
				DisplayName:    "缓冲区",
				EngineType:     "test_workflow_bridge",
				Type:           "spatial",
				Category:       "空间分析",
				CategoryPath:   []string{"空间分析"},
				Description:    "生成缓冲区",
				Parameters:     []plugin.ParameterDescriptor{},
				OutputPorts:    []plugin.OutputPortDescriptor{},
				ExecutionModes: []string{"workflow", "direct"},
			},
		},
	}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	result, err := InvokeOperator(context.Background(), engine, "buffer", plugin.OperatorInvokeRequest{})
	if err != nil {
		t.Fatalf("InvokeOperator returned error: %v", err)
	}
	if result == nil || result.Status != "success" {
		t.Fatalf("unexpected invoke result: %+v", result)
	}
	if !provider.invoked {
		t.Fatal("expected direct-capable operator to be invoked")
	}
}

func TestInvokeOperatorUsesGenericHTTPProviderForCustomWorkflowRuntime(t *testing.T) {
	engineType := "acme_geo_workflow"
	invoked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"operators": []map[string]interface{}{
					testWorkflowOperatorPayload(engineType, "tiff_to_cog", []string{"direct"}),
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/operators/tiff_to_cog/invoke":
			invoked = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":       "success",
				"execution_id": "custom-1",
				"result":       map[string]interface{}{"ok": true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engine := &models.Engine{
		ID:             41,
		Name:           "ACME Geo Workflow",
		EngineType:     engineType,
		EngineOrigin:   "extension",
		IsActive:       true,
		ConnectionInfo: testWorkflowConnectionInfo(t, server.URL),
		Capabilities:   testWorkflowCapabilities(t, engineType),
	}

	result, err := InvokeOperator(context.Background(), engine, "tiff_to_cog", plugin.OperatorInvokeRequest{})
	if err != nil {
		t.Fatalf("InvokeOperator returned error: %v", err)
	}
	if !invoked {
		t.Fatal("expected custom workflow runtime to be invoked")
	}
	if result.ExecutionID != "custom-1" || result.Status != "success" {
		t.Fatalf("unexpected invoke result: %+v", result)
	}
}

func TestTestConnectionUsesGenericHTTPProviderForCustomWorkflowRuntime(t *testing.T) {
	engineType := "acme_geo_workflow"
	healthChecked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		healthChecked = true
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy"})
	}))
	defer server.Close()

	engine := &models.Engine{
		Name:           "ACME Geo Workflow",
		EngineType:     engineType,
		EngineOrigin:   "extension",
		ConnectionInfo: testWorkflowConnectionInfo(t, server.URL),
		Capabilities:   testWorkflowCapabilities(t, engineType),
	}

	if err := TestConnection(context.Background(), engine); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if !healthChecked {
		t.Fatal("expected /health to be checked")
	}
}

func TestProbeWorkflowRuntimeContractRejectsMismatchedOperatorEngineType(t *testing.T) {
	engineType := "acme_geo_workflow"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"operators": []map[string]interface{}{
					testWorkflowOperatorPayload("python_workflow", "tiff_to_cog", []string{"direct"}),
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engine := &models.Engine{
		Name:           "ACME Geo Workflow",
		EngineType:     engineType,
		EngineOrigin:   "extension",
		ConnectionInfo: testWorkflowConnectionInfo(t, server.URL),
		Capabilities:   testWorkflowCapabilities(t, engineType),
	}

	_, err := ProbeWorkflowRuntimeContract(context.Background(), engine)
	if err == nil {
		t.Fatal("ProbeWorkflowRuntimeContract() error = nil, want engine_type mismatch")
	}
	if !strings.Contains(err.Error(), "engine_type=python_workflow") || !strings.Contains(err.Error(), "runtime engine_type=acme_geo_workflow") {
		t.Fatalf("error = %v, want operator/runtime engine_type mismatch", err)
	}
}

func TestResolveDirectWorkflowOperatorFindsCustomRuntimeByOperatorCapability(t *testing.T) {
	engineType := "tenant_raster_workflow"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/operators" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"operators": []map[string]interface{}{
				testWorkflowOperatorPayload(engineType, "tiff_to_cog", []string{"workflow", "direct"}),
			},
		})
	}))
	defer server.Close()

	engines := []models.Engine{
		{
			ID:             10,
			Name:           "Inactive Workflow",
			EngineType:     "inactive_workflow",
			IsActive:       false,
			ConnectionInfo: testWorkflowConnectionInfo(t, server.URL),
			Capabilities:   testWorkflowCapabilities(t, "inactive_workflow"),
		},
		{
			ID:             9,
			Name:           "Tenant Raster Workflow",
			EngineType:     engineType,
			EngineOrigin:   "extension",
			IsActive:       true,
			ConnectionInfo: testWorkflowConnectionInfo(t, server.URL),
			Capabilities:   testWorkflowCapabilities(t, engineType),
		},
	}

	engine, operator, err := ResolveDirectWorkflowOperator(context.Background(), engines, DirectWorkflowOperatorSelector{
		OperatorName: "tiff_to_cog",
	})
	if err != nil {
		t.Fatalf("ResolveDirectWorkflowOperator returned error: %v", err)
	}
	if engine.ID != 9 || operator.Name != "tiff_to_cog" || operator.EngineType != engineType {
		t.Fatalf("unexpected resolved runtime/operator: engine=%+v operator=%+v", engine, operator)
	}
}

func TestResolveDirectWorkflowOperatorDoesNotFallbackToBuiltinWorkflowRuntime(t *testing.T) {
	engineType := "python_workflow"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/operators" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"operators": []map[string]interface{}{
				testWorkflowOperatorPayload(engineType, "buffer", []string{"workflow", "direct"}),
			},
		})
	}))
	defer server.Close()

	engines := []models.Engine{
		{
			ID:             7,
			Name:           "Builtin Python Workflow",
			EngineType:     engineType,
			EngineOrigin:   "extension",
			IsBuiltin:      true,
			IsActive:       true,
			ConnectionInfo: testWorkflowConnectionInfo(t, server.URL),
			Capabilities:   testWorkflowCapabilities(t, engineType),
		},
	}

	_, _, err := ResolveDirectWorkflowOperator(context.Background(), engines, DirectWorkflowOperatorSelector{
		OperatorName: "tiff_to_cog",
	})
	if err == nil {
		t.Fatal("ResolveDirectWorkflowOperator() error = nil, want missing direct operator")
	}
	if !strings.Contains(err.Error(), "direct workflow operator tiff_to_cog is not available") {
		t.Fatalf("error = %v, want direct operator unavailable", err)
	}
}

func TestInvokeOperatorRejectsWorkflowOnlyOperator(t *testing.T) {
	provider := &workflowOperatorProvider{}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	_, err := InvokeOperator(context.Background(), engine, "buffer", plugin.OperatorInvokeRequest{})
	if err == nil {
		t.Fatal("expected workflow-only operator to be rejected")
	}
	if !strings.Contains(err.Error(), "does not support direct invocation") {
		t.Fatalf("expected direct support validation error, got %v", err)
	}
	if provider.invoked {
		t.Fatal("workflow-only operator should not be invoked")
	}
}

func TestExecuteWorkflowAllowsWorkflowOperator(t *testing.T) {
	provider := &workflowOperatorProvider{}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	result, err := ExecuteWorkflow(context.Background(), engine, plugin.WorkflowExecuteRequest{
		WorkflowDef: testWorkflowDefinition("buffer"),
	})
	if err != nil {
		t.Fatalf("ExecuteWorkflow returned error: %v", err)
	}
	if result == nil || result.Status != "success" {
		t.Fatalf("unexpected workflow result: %+v", result)
	}
	if !provider.executed {
		t.Fatal("expected workflow-capable operator to be executed")
	}
}

func TestExecuteWorkflowRejectsDirectOnlyOperator(t *testing.T) {
	provider := &workflowOperatorProvider{
		operators: []plugin.OperatorDescriptor{
			{
				ID:             "buffer",
				Name:           "buffer",
				DisplayName:    "缓冲区",
				EngineType:     "test_workflow_bridge",
				Type:           "spatial",
				Category:       "空间分析",
				CategoryPath:   []string{"空间分析"},
				Description:    "生成缓冲区",
				Parameters:     []plugin.ParameterDescriptor{},
				OutputPorts:    []plugin.OutputPortDescriptor{},
				ExecutionModes: []string{"direct"},
			},
		},
	}
	plugin.Register(provider)
	defer plugin.Unregister(provider.Type())

	engine := &models.Engine{
		EngineType: provider.Type(),
		ConnectionInfo: models.ConnectionInfo{
			"host": "localhost",
			"port": 1,
		},
	}

	_, err := ExecuteWorkflow(context.Background(), engine, plugin.WorkflowExecuteRequest{
		WorkflowDef: testWorkflowDefinition("buffer"),
	})
	if err == nil {
		t.Fatal("expected direct-only operator to be rejected")
	}
	if !strings.Contains(err.Error(), "does not support workflow execution") {
		t.Fatalf("expected workflow support validation error, got %v", err)
	}
	if provider.executed {
		t.Fatal("direct-only operator should not be executed by workflow")
	}
}

func TestExecuteWorkflowRejectsInvalidWorkflowDefinition(t *testing.T) {
	tests := []struct {
		name       string
		workflow   map[string]interface{}
		wantErr    string
		wantCalled bool
	}{
		{
			name: "duplicate task id",
			workflow: workflowDefinitionWithTasks(
				workflowTask("task1", "buffer", map[string]interface{}{}, []interface{}{}),
				workflowTask("task1", "buffer", map[string]interface{}{}, []interface{}{}),
			),
			wantErr: "duplicate task id",
		},
		{
			name: "missing dependency",
			workflow: workflowDefinitionWithTasks(
				workflowTask("task1", "buffer", map[string]interface{}{}, []interface{}{"missing"}),
			),
			wantErr: "depends on missing task",
		},
		{
			name: "ref not listed in depends_on",
			workflow: workflowDefinitionWithTasks(
				workflowTask("task1", "buffer", map[string]interface{}{}, []interface{}{}),
				workflowTask("task2", "buffer", map[string]interface{}{
					"input_gdf": map[string]interface{}{"$ref": "task1"},
				}, []interface{}{}),
			),
			wantErr: "does not list it in depends_on",
		},
		{
			name: "missing output port",
			workflow: workflowDefinitionWithTasks(
				workflowTask("task1", "buffer", map[string]interface{}{}, []interface{}{}),
				workflowTask("task2", "buffer", map[string]interface{}{
					"input_gdf": map[string]interface{}{"$ref": "task1", "port": "large"},
				}, []interface{}{"task1"}),
			),
			wantErr: "references missing output port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &workflowOperatorProvider{}
			plugin.Register(provider)
			defer plugin.Unregister(provider.Type())

			engine := &models.Engine{
				EngineType: provider.Type(),
				ConnectionInfo: models.ConnectionInfo{
					"host": "localhost",
					"port": 1,
				},
			}

			_, err := ExecuteWorkflow(context.Background(), engine, plugin.WorkflowExecuteRequest{
				WorkflowDef: tt.workflow,
			})
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
			if provider.executed != tt.wantCalled {
				t.Fatalf("provider executed = %v, want %v", provider.executed, tt.wantCalled)
			}
		})
	}
}

func testWorkflowDefinition(operator string) map[string]interface{} {
	return workflowDefinitionWithTasks(workflowTask("task1", operator, map[string]interface{}{}, []interface{}{}))
}

func workflowDefinitionWithTasks(tasks ...map[string]interface{}) map[string]interface{} {
	items := make([]interface{}, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, task)
	}
	return map[string]interface{}{"tasks": items}
}

func workflowTask(id, operator string, params map[string]interface{}, dependsOn []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":         id,
		"operator":   operator,
		"params":     params,
		"depends_on": dependsOn,
	}
}

func testWorkflowCapabilities(t *testing.T, engineType string) *models.JSONString {
	t.Helper()
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities(engineType, plugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities: %v", err)
	}
	value := models.JSONString(capabilities)
	return &value
}

func testWorkflowConnectionInfo(t *testing.T, rawURL string) models.ConnectionInfo {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return models.ConnectionInfo{
		"protocol": parsed.Scheme,
		"host":     host,
		"port":     port,
	}
}

func testWorkflowOperatorPayload(engineType, name string, modes []string) map[string]interface{} {
	return map[string]interface{}{
		"id":                name,
		"name":              name,
		"display_name":      name,
		"engine_type":       engineType,
		"type":              "raster",
		"category":          "Raster",
		"category_path":     []string{"Raster"},
		"description":       "Raster operator",
		"brief_description": "Raster operator",
		"execution_modes":   modes,
		"parameters":        []map[string]interface{}{},
		"output_ports": []map[string]interface{}{
			{
				"name":       "default",
				"type":       "object",
				"is_default": true,
			},
		},
	}
}
