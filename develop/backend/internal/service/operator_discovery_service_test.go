package service

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func TestOperatorDiscoveryReturnsWorkflowCapableOperatorsOnly(t *testing.T) {
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities("acme_geo_workflow", "addp.workflow/v1"))
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities() error = %v", err)
	}
	capabilitiesJSON := commonModels.JSONString(capabilities)
	engine := &commonModels.Engine{
		ID:           12,
		Name:         "acme geo workflow",
		EngineType:   "geopython_workflow",
		IsActive:     true,
		Capabilities: &capabilitiesJSON,
	}

	service := &OperatorDiscoveryService{
		getEngineByID: func(id uint) (*commonModels.Engine, error) {
			if id != engine.ID {
				t.Fatalf("engine id = %d, want %d", id, engine.ID)
			}
			return engine, nil
		},
		listWorkflowOperators: func(ctx context.Context, engine *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
			return []commonModels.OperatorDescriptor{
				{
					ID:             "load",
					Name:           "load",
					ExecutionModes: []string{"workflow"},
					Parameters: []commonModels.ParameterDescriptor{
						{Name: "connection_info", Type: "object"},
						{Name: "schema", Type: "string"},
						{Name: "table", Type: "string"},
						{Name: "path", Type: "string"},
					},
				},
				{
					ID:             "tiff_to_cog",
					Name:           "tiff_to_cog",
					ExecutionModes: []string{"workflow", "direct"},
					Parameters: []commonModels.ParameterDescriptor{
						{Name: "path", Type: "string"},
					},
				},
				{
					ID:             "direct_only",
					Name:           "direct_only",
					ExecutionModes: []string{"direct"},
				},
			}, nil
		},
	}

	operators, err := service.GetOperatorsByWorkflowEngineID(context.Background(), engine.ID)
	if err != nil {
		t.Fatalf("GetOperatorsByWorkflowEngineID() error = %v", err)
	}
	if len(operators) != 2 {
		t.Fatalf("operators len = %d, want 2: %+v", len(operators), operators)
	}
	if operators[0].Name != "load" || operators[1].Name != "tiff_to_cog" {
		t.Fatalf("unexpected operators: %+v", operators)
	}
	publicNames := map[string]bool{}
	for _, parameter := range operators[0].PublicParameters {
		publicNames[parameter.Name] = true
	}
	for _, name := range []string{"数据源", "locator"} {
		if !publicNames[name] {
			t.Fatalf("load public parameters = %+v, missing %s", operators[0].PublicParameters, name)
		}
	}
	for _, name := range []string{"connection_info", "schema", "table"} {
		if publicNames[name] {
			t.Fatalf("load public parameters = %+v, runtime param %s leaked", operators[0].PublicParameters, name)
		}
	}
	for _, name := range []string{"source_type", "format", "geojson", "文件"} {
		if publicNames[name] {
			t.Fatalf("load public parameters = %+v, obsolete param %s leaked", operators[0].PublicParameters, name)
		}
	}
	if len(operators[0].Parameters) != 4 {
		t.Fatalf("runtime parameters should remain intact for runtime contract: %+v", operators[0].Parameters)
	}
	assertResourceBinding(t, operators[0].PublicParameters, "数据源", map[string]interface{}{
		"mode":                  "existing",
		"locator_param":         "locator",
		"geometry_column_param": "geom_column",
	})
	loadPicker := parameterByName(t, operators[0].PublicParameters, "数据源")
	formats, ok := loadPicker.UIConfig["file_formats"].([]string)
	if !ok || len(formats) == 0 {
		t.Fatalf("load file_formats = %#v, want non-empty string list", loadPicker.UIConfig["file_formats"])
	}
	if len(operators[1].PublicParameters) != 1 || operators[1].PublicParameters[0].Name != "path" {
		t.Fatalf("undeclared operator public parameters = %+v, want explicit runtime path preserved", operators[1].PublicParameters)
	}
}

func TestOperatorDiscoveryPublishesTargetResourceBinding(t *testing.T) {
	operators := []commonModels.OperatorDescriptor{{
		ID: "save",
		Parameters: []commonModels.ParameterDescriptor{
			{Name: "mode", Type: "string"},
			{Name: "connection_info", Type: "object"},
			{Name: "schema", Type: "string"},
			{Name: "table", Type: "string"},
			{Name: "path", Type: "string"},
		},
	}}

	publicOperators := publicWorkflowOperators("geopython_workflow", operators)
	if len(publicOperators) != 1 {
		t.Fatalf("operators len = %d, want 1", len(publicOperators))
	}
	assertResourceBinding(t, publicOperators[0].PublicParameters, "保存目标", map[string]interface{}{
		"mode":                 "target",
		"parent_locator_param": "target_parent_locator",
		"name_param":           "target_name",
	})
	publicNames := map[string]bool{}
	for _, parameter := range publicOperators[0].PublicParameters {
		publicNames[parameter.Name] = true
	}
	for _, name := range []string{"target_type", "format", "文件目标"} {
		if publicNames[name] {
			t.Fatalf("save public parameters = %+v, obsolete param %s leaked", publicOperators[0].PublicParameters, name)
		}
	}
}

func TestOperatorDiscoveryPublishesSuperMapUdbxNFSTargetOnly(t *testing.T) {
	operators := []commonModels.OperatorDescriptor{{
		ID: "datasource.create",
		Parameters: []commonModels.ParameterDescriptor{
			{Name: "connection_info", Type: "object"},
			{Name: "path", Type: "string"},
		},
	}}

	publicOperators := publicWorkflowOperators("supermap_workflow", operators)
	if len(publicOperators) != 1 {
		t.Fatalf("operators len = %d, want 1", len(publicOperators))
	}
	picker := parameterByName(t, publicOperators[0].PublicParameters, "UDBX 保存目录")
	families, ok := picker.UIConfig["engine_families"].([]string)
	if !ok || len(families) != 1 || families[0] != "file" {
		t.Fatalf("engine_families = %#v, want file only", picker.UIConfig["engine_families"])
	}
	engineTypes, ok := picker.UIConfig["engine_types"].([]string)
	if !ok || len(engineTypes) != 1 || engineTypes[0] != "nfs" {
		t.Fatalf("engine_types = %#v, want nfs only", picker.UIConfig["engine_types"])
	}
	parentTypes, ok := picker.UIConfig["selectable_parent_node_types"].([]string)
	if !ok {
		t.Fatalf("selectable_parent_node_types = %#v, want string list", picker.UIConfig["selectable_parent_node_types"])
	}
	for _, parentType := range parentTypes {
		if parentType == "bucket" || parentType == "prefix" {
			t.Fatalf("SuperMap UDBX target should not expose object storage parent type: %#v", parentTypes)
		}
	}
}

func assertResourceBinding(t *testing.T, parameters []commonModels.ParameterDescriptor, parameterName string, expected map[string]interface{}) {
	t.Helper()
	parameter := parameterByName(t, parameters, parameterName)
	binding, ok := parameter.UIConfig["resource_binding"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameter %s resource_binding = %#v, want object", parameterName, parameter.UIConfig["resource_binding"])
	}
	for key, value := range expected {
		if binding[key] != value {
			t.Fatalf("parameter %s resource_binding[%s] = %#v, want %#v", parameterName, key, binding[key], value)
		}
	}
}

func parameterByName(t *testing.T, parameters []commonModels.ParameterDescriptor, parameterName string) commonModels.ParameterDescriptor {
	t.Helper()
	for _, parameter := range parameters {
		if parameter.Name == parameterName {
			return parameter
		}
	}
	t.Fatalf("public parameters = %+v, missing %s", parameters, parameterName)
	return commonModels.ParameterDescriptor{}
}

func TestValidateWorkflowOperatorContractsRejectsPublicResourceParamInRuntimeSpec(t *testing.T) {
	err := validateWorkflowOperatorContracts("geopython_workflow", []commonModels.OperatorDescriptor{{
		ID: "load",
		Parameters: []commonModels.ParameterDescriptor{
			{Name: "locator", Type: "string"},
		},
	}})
	if err == nil {
		t.Fatal("validateWorkflowOperatorContracts() error = nil, want public resource param error")
	}
}

func TestValidateWorkflowOperatorContractsRequiresAdapterRuntimeParams(t *testing.T) {
	err := validateWorkflowOperatorContracts("supermap_workflow", []commonModels.OperatorDescriptor{{
		ID: "datasource.open_postgis",
		Parameters: []commonModels.ParameterDescriptor{
			{Name: "connection_info", Type: "object"},
			{Name: "schema", Type: "string"},
		},
	}})
	if err == nil {
		t.Fatal("validateWorkflowOperatorContracts() error = nil, want missing table error")
	}
}

func TestValidateWorkflowOperatorContractsAcceptsSuperMapS3MAccessPlanOnly(t *testing.T) {
	err := validateWorkflowOperatorContracts("supermap_workflow", []commonModels.OperatorDescriptor{{
		ID: "osgb_scene_to_s3m",
		Parameters: []commonModels.ParameterDescriptor{
			{Name: "access_plan", Type: "object"},
		},
	}})
	if err != nil {
		t.Fatalf("validateWorkflowOperatorContracts() error = %v, want nil", err)
	}
}

func TestValidateWorkflowOperatorContractsRejectsDuplicateRuntimeParam(t *testing.T) {
	err := validateWorkflowOperatorContracts("acme_workflow", []commonModels.OperatorDescriptor{{
		ID: "duplicate",
		Parameters: []commonModels.ParameterDescriptor{
			{Name: "mode", Type: "string"},
			{Name: "mode", Type: "string"},
		},
	}})
	if err == nil {
		t.Fatal("validateWorkflowOperatorContracts() error = nil, want duplicate parameter error")
	}
}

func TestValidateWorkflowAcceptsPublicOperatorParameters(t *testing.T) {
	service := newWorkflowValidationTestService(t)
	result, err := service.ValidateWorkflow(context.Background(), 12, map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":         "buffer_1",
				"operator":   "buffer",
				"params":     map[string]interface{}{"distance": float64(50)},
				"depends_on": []interface{}{},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateWorkflow() error = %v", err)
	}
	if !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("ValidateWorkflow() result = %+v, want valid", result)
	}
}

func TestValidateWorkflowRejectsUnknownOperatorAndPrivateParameter(t *testing.T) {
	service := newWorkflowValidationTestService(t)
	result, err := service.ValidateWorkflow(context.Background(), 12, map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"id":         "unknown_1",
				"operator":   "missing",
				"params":     map[string]interface{}{},
				"depends_on": []interface{}{},
			},
			map[string]interface{}{
				"id":         "buffer_1",
				"operator":   "buffer",
				"params":     map[string]interface{}{"connection_info": map[string]interface{}{}},
				"depends_on": []interface{}{"unknown_1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateWorkflow() error = %v", err)
	}
	if result.Valid {
		t.Fatalf("ValidateWorkflow() result = %+v, want invalid", result)
	}
	codes := map[string]bool{}
	for _, issue := range result.Errors {
		codes[issue.Code] = true
	}
	for _, code := range []string{"operator_not_found", "required_parameter_missing", "parameter_not_public"} {
		if !codes[code] {
			t.Fatalf("ValidateWorkflow() errors = %+v, missing code %s", result.Errors, code)
		}
	}
}

func TestDevExecutorRejectsWorkflowBeforeCreatingExecution(t *testing.T) {
	executor := &DevExecutor{operatorDiscovery: newWorkflowValidationTestService(t)}
	err := executor.validateWorkflowBeforeExecution(
		context.Background(),
		map[string]interface{}{
			"workflow_definition": map[string]interface{}{
				"tasks": []interface{}{
					map[string]interface{}{
						"id":         "buffer_1",
						"operator":   "missing",
						"params":     map[string]interface{}{},
						"depends_on": []interface{}{},
					},
				},
			},
		},
		map[string]interface{}{"engine_id": float64(12)},
		3,
	)
	if err == nil {
		t.Fatal("validateWorkflowBeforeExecution() error = nil, want validation failure")
	}
}

func TestOperatorDiscoveryRejectsAnotherTenantEngine(t *testing.T) {
	service := newWorkflowValidationTestService(t)
	tenantID := uint(9)
	service.getEngineByID = func(uint) (*commonModels.Engine, error) {
		engine, _ := newWorkflowValidationTestService(t).getEngineByID(12)
		engine.TenantID = &tenantID
		return engine, nil
	}

	_, err := service.GetOperatorsByWorkflowEngineIDForTenant(context.Background(), 12, 3)
	if err == nil {
		t.Fatal("GetOperatorsByWorkflowEngineIDForTenant() error = nil, want access denial")
	}
}

func newWorkflowValidationTestService(t *testing.T) *OperatorDiscoveryService {
	t.Helper()
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities("acme_workflow", "addp.workflow/v1"))
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities() error = %v", err)
	}
	capabilitiesJSON := commonModels.JSONString(capabilities)
	engine := &commonModels.Engine{
		ID:           12,
		Name:         "acme workflow",
		EngineType:   "acme_workflow",
		IsActive:     true,
		Capabilities: &capabilitiesJSON,
	}
	return &OperatorDiscoveryService{
		getEngineByID: func(id uint) (*commonModels.Engine, error) {
			return engine, nil
		},
		listWorkflowOperators: func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
			return []commonModels.OperatorDescriptor{{
				ID:             "buffer",
				Name:           "buffer",
				ExecutionModes: []string{"workflow"},
				Parameters: []commonModels.ParameterDescriptor{{
					Name: "distance", Type: "float", Required: true,
				}},
			}}, nil
		},
	}
}
