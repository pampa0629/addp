package service

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
)

var additionalConversionCases = []struct {
	engineType, operator, sourceFormat, targetName string
}{
	{"model3d_workflow", "dae_to_glb", "dae", "scene.glb"},
	{"model3d_workflow", "3ds_to_glb", "3ds", "scene.glb"},
	{"model3d_workflow", "max_to_glb", "max", "scene.glb"},
	{"supermap_workflow", "sgm_to_osgb", "sgm", "scene.osgb"},
	{"document_workflow", "document_to_pdf", "pptx", "slides.pdf"},
}

func additionalConversionDiscovery(t *testing.T, engineType, operator string) *OperatorDiscoveryService {
	t.Helper()
	discovery := newWorkflowValidationTestService(t)
	getDescriptor := discovery.getRuntimeDescriptor
	discovery.getRuntimeDescriptor = func(ctx context.Context, tenantID, engineID uint) (*commonModels.EngineRuntimeDescriptor, error) {
		descriptor, err := getDescriptor(ctx, tenantID, engineID)
		descriptor.EngineType = engineType
		return descriptor, err
	}
	discovery.listWorkflowOperators = func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
		parameters := []commonModels.ParameterDescriptor{{Name: "access_plan", Type: "object", Required: true}}
		if operator != "sgm_to_osgb" {
			parameters = append(parameters, commonModels.ParameterDescriptor{Name: "options", Type: "object"})
		}
		return []commonModels.OperatorDescriptor{{
			ID: operator, Name: operator, DisplayName: operator, EngineType: engineType,
			ExecutionModes: []string{"workflow", "direct"}, Effects: []string{"read", "write"}, Parameters: parameters,
		}}, nil
	}
	return discovery
}

func additionalConversionWorkflow(operator, sourceFormat, targetName string) map[string]interface{} {
	return map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
		"id": "convert", "operator": operator, "depends_on": []interface{}{},
		"params": map[string]interface{}{
			"locator":               "addp://engine/1/path/source/scene." + sourceFormat + "?type=file&item_id=8",
			"target_parent_locator": "addp://engine/2/path/business/results?type=prefix",
			"target_name":           targetName, "write_mode": "create",
		},
	}}}
}

func TestAdditionalConversionsPublishResourceControlsAndStableOutputs(t *testing.T) {
	for _, tc := range additionalConversionCases {
		t.Run(tc.operator, func(t *testing.T) {
			discovery := additionalConversionDiscovery(t, tc.engineType, tc.operator)
			operators, err := discovery.GetOperatorsByWorkflowEngineIDForTenant(context.Background(), 12, 7)
			if err != nil || len(operators) != 1 {
				t.Fatalf("discovery = %#v, err = %v", operators, err)
			}
			parameters := operators[0].PublicParameters
			for _, parameter := range parameters {
				if parameter.Name == "access_plan" || parameter.Name == "options" {
					t.Fatalf("runtime parameter exposed publicly: %#v", parameter)
				}
			}
			for _, name := range []string{"source_resource", "target_resource"} {
				picker := parameterByName(t, parameters, name)
				types, _ := picker.UIConfig["engine_types"].([]string)
				if picker.UIType != "resource_tree_picker" || !slices.Contains(types, "nfs") || !slices.Contains(types, "minio") || slices.Contains(types, "postgresql") {
					t.Fatalf("picker = %#v", picker)
				}
			}
			workflow := additionalConversionWorkflow(tc.operator, tc.sourceFormat, tc.targetName)
			validation, err := discovery.ValidateWorkflowForTenant(context.Background(), 12, workflow, 7)
			if err != nil || !validation.Valid {
				t.Fatalf("public workflow validation = %#v, err = %v", validation, err)
			}
			contract, err := discovery.WorkflowExecutionContractForTenant(context.Background(), 12, workflow, 7)
			if err != nil {
				t.Fatal(err)
			}
			if _, exists := contract.OutputSchema["properties"].(map[string]interface{})["convert"]; !exists {
				t.Fatalf("missing stable resource output: %#v", contract.OutputSchema)
			}
			input := contract.InputSchema["properties"].(map[string]interface{})["convert"].(map[string]interface{})["properties"].(map[string]interface{})
			if input["access_plan"] != nil || input["options"] != nil || input["source_resource"] == nil || input["target_resource"] == nil {
				t.Fatalf("invalid execution input schema: %#v", input)
			}
			firstTaskParams(t, workflow)["access_plan"] = map[string]interface{}{}
			validation, err = discovery.ValidateWorkflowForTenant(context.Background(), 12, workflow, 7)
			if err != nil || validation.Valid {
				t.Fatalf("private access plan must be rejected: %#v, err = %v", validation, err)
			}
		})
	}
}

func TestAdditionalConversionOptionsWithObjectSourceAndMountedTarget(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		1: {ID: 1, EngineType: "minio", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "minio:9000", "access_key": "key", "secret_key": "secret"}},
		2: {ID: 2, EngineType: "nfs", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"mount_path": "/business"}},
	})
	for _, tc := range []struct {
		engineType, operator, format, target, kind, entrypoint string
		options                                                commonModels.JSONMap
	}{
		{"model3d_workflow", "max_to_glb", "max", "scene.glb", "directory", "scene.max", commonModels.JSONMap{"source_unit": "cm", "texture_files": map[string]interface{}{"wall.png": "textures/wall.png"}}},
		{"document_workflow", "document_to_pdf", "pptx", "slides.pdf", "file", "", commonModels.JSONMap{"strip_embedded_media": false}},
	} {
		t.Run(tc.operator, func(t *testing.T) {
			workflow := additionalConversionWorkflow(tc.operator, tc.format, tc.target)
			params := firstTaskParams(t, workflow)
			params["locator"] = "addp://engine/1/path/bucket/models/scene." + tc.format + "?type=object&item_id=8"
			params["target_parent_locator"] = "addp://engine/2/path/results?type=directory"
			for name, value := range tc.options {
				params[name] = value
			}
			got, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, tc.engineType, workflow)
			if err != nil {
				t.Fatal(err)
			}
			runtime := firstTaskParams(t, got)
			if !reflect.DeepEqual(runtime["options"], tc.options) {
				t.Fatalf("options = %#v, want %#v", runtime["options"], tc.options)
			}
			for name := range tc.options {
				if _, exists := runtime[name]; exists {
					t.Fatalf("public option %s leaked into runtime top level", name)
				}
				if !reflect.DeepEqual(params[name], tc.options[name]) {
					t.Fatalf("saved workflow option %s mutated", name)
				}
			}
			plan := runtime["access_plan"].(commonModels.JSONMap)
			source := plan["source"].(commonModels.JSONMap)
			target := plan["target"].(commonModels.JSONMap)
			if source["kind"] != tc.kind || stringParam(source, "entrypoint") != tc.entrypoint ||
				source["access"].(commonModels.JSONMap)["method"] != "object_store" ||
				target["access"].(commonModels.JSONMap)["method"] != "mounted_path" {
				t.Fatalf("invalid source/target access plan: %#v", plan)
			}
			if len(targets) != 1 || targets[0].TaskID != "convert" || targets[0].EngineID != 2 || targets[0].WriteMode != "create" {
				t.Fatalf("invalid produced targets: %#v", targets)
			}
		})
	}
}

func TestAdditionalConversionsAuthorizeSourceAndTargetSeparately(t *testing.T) {
	for _, tc := range additionalConversionCases {
		t.Run(tc.operator, func(t *testing.T) {
			executor := &DevExecutor{operatorDiscovery: additionalConversionDiscovery(t, tc.engineType, tc.operator)}
			plan, err := executor.buildWorkflowExecutionAuthorizationPlan(context.Background(), &models.DevTask{
				DevType: "workflow", ExecutionConfig: models.DevTaskContent{"engine_id": float64(12)},
				Content: models.DevTaskContent{"workflow_definition": additionalConversionWorkflow(tc.operator, tc.sourceFormat, tc.targetName)},
			}, 7)
			if err != nil {
				t.Fatal(err)
			}
			want := map[uint][]string{1: {"read"}, 2: {"write"}, 12: {"read", "write"}}
			if !reflect.DeepEqual(plan.engineEffects, want) {
				t.Fatalf("effects = %#v, want %#v", plan.engineEffects, want)
			}
		})
	}
}

func TestAdditionalConversionsRejectRuntimeOverridesAndInvalidTargetNames(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		1: {ID: 1, EngineType: "nfs", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"mount_path": "/data"}},
		2: {ID: 2, EngineType: "minio", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "minio:9000", "access_key": "key", "secret_key": "secret"}},
	})
	for _, tc := range additionalConversionCases {
		t.Run(tc.operator, func(t *testing.T) {
			for _, name := range []string{"access_plan", "options"} {
				if name == "options" && tc.operator == "sgm_to_osgb" {
					continue
				}
				workflow := additionalConversionWorkflow(tc.operator, tc.sourceFormat, tc.targetName)
				firstTaskParams(t, workflow)[name] = map[string]interface{}{}
				_, _, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, tc.engineType, workflow)
				if err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("runtime override %s error = %v", name, err)
				}
			}
			workflow := additionalConversionWorkflow(tc.operator, tc.sourceFormat, "wrong.txt")
			if _, _, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, tc.engineType, workflow); err == nil {
				t.Fatal("invalid target extension accepted")
			}
		})
	}
}

func TestConversionOptionLocalizationDoesNotMutateRegistry(t *testing.T) {
	for _, tc := range []struct{ engineType, operator, parameter, chinese, english string }{
		{"model3d_workflow", "max_to_glb", "source_unit", "源模型单位", "Source model unit"},
		{"model3d_workflow", "max_to_glb", "texture_files", "贴图映射", "Texture mapping"},
		{"document_workflow", "document_to_pdf", "strip_embedded_media", "移除嵌入媒体", "Remove embedded media"},
	} {
		t.Run(tc.parameter, func(t *testing.T) {
			discovery := additionalConversionDiscovery(t, tc.engineType, tc.operator)
			operators, err := discovery.GetOperatorsByWorkflowEngineIDForTenant(context.Background(), 12, 7)
			if err != nil {
				t.Fatal(err)
			}
			if got := parameterByName(t, operators[0].PublicParameters, tc.parameter); got.DisplayName != tc.chinese {
				t.Fatalf("default parameter = %#v", got)
			}
			operators[0].LocalizeAdapterParameters(tc.engineType, "en")
			if got := parameterByName(t, operators[0].PublicParameters, tc.parameter); got.DisplayName != tc.english || strings.HasPrefix(got.Description, "develop.") {
				t.Fatalf("English parameter = %#v", got)
			}
			operators[0].LocalizeAdapterParameters(tc.engineType, "zh-cn")
			if got := parameterByName(t, operators[0].PublicParameters, tc.parameter); got.DisplayName != tc.chinese {
				t.Fatalf("Chinese parameter = %#v", got)
			}
			fresh, _ := discovery.GetOperatorsByWorkflowEngineIDForTenant(context.Background(), 12, 7)
			if got := parameterByName(t, fresh[0].PublicParameters, tc.parameter); got.DisplayName != tc.chinese {
				t.Fatalf("registry mutated: %#v", got)
			}
		})
	}
}

func TestAdditionalConversionOptionsCanBeOverriddenForOneExecution(t *testing.T) {
	for _, tc := range []struct {
		engineType, operator, format, target string
		saved, override                      map[string]interface{}
	}{
		{"model3d_workflow", "max_to_glb", "max", "scene.glb",
			map[string]interface{}{"source_unit": "m", "texture_files": map[string]interface{}{}},
			map[string]interface{}{"source_unit": "cm", "texture_files": map[string]interface{}{"wall.png": "textures/wall.png"}}},
		{"document_workflow", "document_to_pdf", "pptx", "slides.pdf",
			map[string]interface{}{"strip_embedded_media": true}, map[string]interface{}{"strip_embedded_media": false}},
	} {
		t.Run(tc.operator, func(t *testing.T) {
			discovery := additionalConversionDiscovery(t, tc.engineType, tc.operator)
			workflow := additionalConversionWorkflow(tc.operator, tc.format, tc.target)
			for name, value := range tc.saved {
				firstTaskParams(t, workflow)[name] = value
			}
			resolved, err := discovery.resolveWorkflowExecutionParameters(context.Background(), 12, workflow,
				map[string]interface{}{"convert": tc.override}, 7)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range tc.override {
				if !reflect.DeepEqual(firstTaskParams(t, resolved.Workflow)[name], value) ||
					!reflect.DeepEqual(firstTaskParams(t, workflow)[name], tc.saved[name]) {
					t.Fatalf("execution override %s did not remain isolated", name)
				}
			}
			invalid := map[string]interface{}{"strip_embedded_media": "false"}
			if tc.operator == "max_to_glb" {
				invalid = map[string]interface{}{"source_unit": "invalid-unit"}
			}
			if _, err := discovery.resolveWorkflowExecutionParameters(context.Background(), 12, workflow,
				map[string]interface{}{"convert": invalid}, 7); err == nil {
				t.Fatal("invalid conversion option accepted")
			}
		})
	}
}
