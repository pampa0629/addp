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
			{Name: "target_type", Type: "string"},
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
