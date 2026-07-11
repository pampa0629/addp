package service

import (
	"fmt"

	commonModels "github.com/addp/common/models"
)

type workflowResourceInputSpec struct {
	PublicParam   string
	RuntimeParams []string
}

type workflowResourceOutputSpec struct {
	ParentParam   string
	NameParam     string
	RuntimeParams []string
}

type workflowOperatorAdapterSpec struct {
	OperatorID       string
	PublicParameters []commonModels.ParameterDescriptor
	ResourceInputs   []workflowResourceInputSpec
	ResourceOutputs  []workflowResourceOutputSpec
}

var workflowOperatorAdapterSpecs = map[string]map[string]workflowOperatorAdapterSpec{
	"geopython_workflow": {
		"load": workflowPythonLoadAdapterSpec(),
		"save": workflowSaveAdapterSpec("save"),
	},
	"spark_workflow": {
		"load": workflowLoadAdapterSpec("load"),
		"save": workflowSaveAdapterSpec("save"),
	},
	"supermap_workflow": {
		"datasource.open_postgis": {
			OperatorID: "datasource.open_postgis",
			PublicParameters: []commonModels.ParameterDescriptor{
				resourcePickerParameter(
					"数据源",
					"选择已有 PostGIS 空间表。",
					nil,
					map[string]interface{}{
						"api_base_url":              "/api/v1/meta",
						"engine_families":           []string{"tabular"},
						"selectable_node_types":     []string{"table"},
						"enable_geometry_detection": true,
						"require_geometry":          true,
						"resource_binding": map[string]interface{}{
							"mode":          "existing",
							"locator_param": "locator",
							"type_values": map[string]interface{}{
								"table": "table", "collection": "table",
							},
						},
					},
				),
				resourceIdentityParameter("locator", "源表 ResourceLocator"),
			},
			ResourceInputs: []workflowResourceInputSpec{{
				PublicParam:   "locator",
				RuntimeParams: []string{"engine_id", "connection_info", "schema", "table"},
			}},
		},
	},
}

func workflowPythonLoadAdapterSpec() workflowOperatorAdapterSpec {
	return workflowOperatorAdapterSpec{
		OperatorID: "load",
		PublicParameters: []commonModels.ParameterDescriptor{
			resourcePickerParameter(
				"数据源",
				"选择已有数据库表、文件或对象。",
				nil,
				map[string]interface{}{
					"api_base_url":          "/api/v1/meta",
					"engine_families":       []string{"tabular", "dynamic_schema", "file", "object"},
					"selectable_node_types": []string{"table", "collection", "file", "object"},
					"file_formats":          []string{"csv", "parquet", "xlsx", "json", "feather", "shp", "geojson", "gpkg", "kml", "gml", "fgb"},
					"resource_binding": map[string]interface{}{
						"mode":                  "existing",
						"locator_param":         "locator",
						"geometry_column_param": "geom_column",
					},
				},
			),
			resourceIdentityParameter("locator", "源资源 ResourceLocator"),
		},
		ResourceInputs: []workflowResourceInputSpec{{
			PublicParam:   "locator",
			RuntimeParams: []string{"engine_id", "connection_info", "schema", "table", "path"},
		}},
	}
}

func workflowLoadAdapterSpec(operatorID string) workflowOperatorAdapterSpec {
	return workflowOperatorAdapterSpec{
		OperatorID: operatorID,
		PublicParameters: []commonModels.ParameterDescriptor{
			resourcePickerParameter(
				"数据源",
				"选择已有数据库表。",
				map[string]interface{}{"source_type": "table"},
				map[string]interface{}{
					"api_base_url":              "/api/v1/meta",
					"engine_families":           []string{"tabular", "dynamic_schema"},
					"selectable_node_types":     []string{"table", "collection"},
					"enable_geometry_detection": true,
					"require_geometry":          false,
					"resource_binding": map[string]interface{}{
						"mode":          "existing",
						"locator_param": "locator",
						"type_param":    "source_type",
						"type_values": map[string]interface{}{
							"table": "table", "collection": "table",
						},
					},
				},
			),
			resourcePickerParameter(
				"文件",
				"选择已有文件或对象。",
				map[string]interface{}{"source_type": "file"},
				map[string]interface{}{
					"api_base_url":          "/api/v1/meta",
					"engine_families":       []string{"file", "object"},
					"selectable_node_types": []string{"file", "object"},
					"resource_binding": map[string]interface{}{
						"mode":          "existing",
						"locator_param": "locator",
						"type_param":    "source_type",
						"type_values": map[string]interface{}{
							"file": "file", "object": "file",
						},
					},
				},
			),
			resourceIdentityParameter("locator", "源资源 ResourceLocator"),
		},
		ResourceInputs: []workflowResourceInputSpec{{
			PublicParam:   "locator",
			RuntimeParams: []string{"engine_id", "connection_info", "schema", "table", "path"},
		}},
	}
}

func workflowSaveAdapterSpec(operatorID string) workflowOperatorAdapterSpec {
	return workflowOperatorAdapterSpec{
		OperatorID: operatorID,
		PublicParameters: []commonModels.ParameterDescriptor{
			resourcePickerParameter(
				"保存目标",
				"选择目标 Schema 或数据库。",
				map[string]interface{}{"target_type": "table"},
				map[string]interface{}{
					"api_base_url":                 "/api/v1/meta",
					"engine_families":              []string{"tabular", "dynamic_schema"},
					"selectable_parent_node_types": []string{"schema", "database"},
					"allow_create_table":           true,
					"resource_binding": map[string]interface{}{
						"mode":                 "target",
						"parent_locator_param": "target_parent_locator",
						"name_param":           "target_name",
						"type_param":           "target_type",
						"type_values": map[string]interface{}{
							"schema": "table", "database": "table",
						},
						"default_params": map[string]interface{}{"mode": "replace"},
					},
				},
			),
			resourcePickerParameter(
				"文件目标",
				"选择目标目录、Bucket 或 Prefix。",
				map[string]interface{}{"target_type": "file"},
				map[string]interface{}{
					"api_base_url":                 "/api/v1/meta",
					"engine_families":              []string{"file", "object"},
					"selectable_parent_node_types": []string{"root", "directory", "dir", "bucket", "prefix"},
					"resource_binding": map[string]interface{}{
						"mode":                 "target",
						"parent_locator_param": "target_parent_locator",
						"name_param":           "target_name",
						"type_param":           "target_type",
						"type_values": map[string]interface{}{
							"root": "file", "directory": "file", "dir": "file", "bucket": "file", "prefix": "file",
						},
						"default_params": map[string]interface{}{"mode": "replace"},
					},
				},
			),
			resourceIdentityParameter("target_parent_locator", "目标父节点 ResourceLocator"),
			resourceIdentityParameter("target_name", "目标名称"),
		},
		ResourceOutputs: []workflowResourceOutputSpec{{
			ParentParam:   "target_parent_locator",
			NameParam:     "target_name",
			RuntimeParams: []string{"engine_id", "connection_info", "schema", "table", "path"},
		}},
	}
}

func resourcePickerParameter(name, description string, showWhen map[string]interface{}, uiConfig map[string]interface{}) commonModels.ParameterDescriptor {
	return commonModels.ParameterDescriptor{
		Name:        name,
		Type:        "ui",
		ParamType:   "ui",
		Description: description,
		ShowWhen:    showWhen,
		UIType:      "resource_tree_picker",
		UIConfig:    uiConfig,
	}
}

func resourceIdentityParameter(name, description string) commonModels.ParameterDescriptor {
	return commonModels.ParameterDescriptor{
		Name:        name,
		Type:        "string",
		ParamType:   "resource",
		Description: description,
	}
}

func workflowOperatorAdapterSpecFor(engineType, operatorID string) (workflowOperatorAdapterSpec, bool) {
	engineSpecs, ok := workflowOperatorAdapterSpecs[engineType]
	if !ok {
		return workflowOperatorAdapterSpec{}, false
	}
	spec, ok := engineSpecs[operatorID]
	return spec, ok
}

func rejectUndeclaredWorkflowResourceParams(engineType, operatorID string, params map[string]interface{}) error {
	for _, param := range []string{"locator", "target_parent_locator", "target_name"} {
		if _, ok := params[param]; ok {
			return fmt.Errorf("workflow engine %s operator %s 未声明 Develop Adapter Spec，不允许提交资源参数 %s", engineType, operatorID, param)
		}
	}
	return nil
}

func workflowAdapterRuntimeParams(spec workflowOperatorAdapterSpec) map[string]struct{} {
	result := map[string]struct{}{
		"engine_id":       {},
		"connection_info": {},
	}
	for _, input := range spec.ResourceInputs {
		for _, param := range input.RuntimeParams {
			result[param] = struct{}{}
		}
	}
	for _, output := range spec.ResourceOutputs {
		for _, param := range output.RuntimeParams {
			result[param] = struct{}{}
		}
	}
	return result
}

func rejectDirectWorkflowRuntimeParams(params map[string]interface{}, spec workflowOperatorAdapterSpec) error {
	for param := range workflowAdapterRuntimeParams(spec) {
		if _, ok := params[param]; ok {
			return fmt.Errorf("不允许直接提交运行时参数 %s，请使用 adapter spec 声明的公开资源参数", param)
		}
	}
	return nil
}
