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
	RuntimeParams    []string
	AccessPlan       *workflowAccessPlanSpec
}

type workflowAccessPlanSpec struct {
	SourceFormat      string
	SourceKind        string
	SourceScope       string
	SourceDataTypes   []string
	TargetFormat      string
	TargetKind        string
	TargetExtension   string
	TargetContentType string
	OptionParams      []string
}

var workflowOperatorAdapterSpecs = map[string]map[string]workflowOperatorAdapterSpec{
	"geopython_workflow": {
		"load": workflowPythonLoadAdapterSpec(),
		"save": workflowPythonSaveAdapterSpec(),
	},
	"spark_workflow": {
		"load": workflowLoadAdapterSpec("load"),
		"save": workflowSaveAdapterSpec("save"),
	},
	"supermap_workflow": {
		"datasource.open_postgis": workflowSuperMapOpenPostgisAdapterSpec(),
		"datasource.create":       workflowSuperMapCreateUdbxAdapterSpec(),
		"osgb_scene_to_s3m":       workflowSuperMapS3MAdapterSpec(),
	},
	"model3d_workflow":    model3DWorkflowAdapterSpecs(),
	"pointcloud_workflow": pointCloudWorkflowAdapterSpecs(),
}

func workflowSuperMapS3MAdapterSpec() workflowOperatorAdapterSpec {
	spec := conversionAdapterSpec(
		"osgb_scene_to_s3m", "OSGB Scene", "osgb_scene", "directory", "directory", nil,
		"s3m", "directory", "", "", nil,
	)
	spec.RuntimeParams = []string{"access_plan"}
	for index := range spec.PublicParameters {
		parameter := &spec.PublicParameters[index]
		if parameter.UIType == "resource_tree_picker" {
			binding, _ := parameter.UIConfig["resource_binding"].(map[string]interface{})
			if binding["mode"] == "existing" {
				parameter.UIConfig["engine_families"] = []string{"file"}
				parameter.UIConfig["engine_types"] = []string{"nfs"}
			} else {
				parameter.UIConfig["engine_families"] = []string{"file", "object"}
				delete(parameter.UIConfig, "engine_types")
			}
		}
	}
	return spec
}

func model3DWorkflowAdapterSpecs() map[string]workflowOperatorAdapterSpec {
	return map[string]workflowOperatorAdapterSpec{
		"osgb_to_glb":           conversionAdapterSpec("osgb_to_glb", "OSGB 模型", "osgb", "file", "file", nil, "glb", "file", ".glb", "model/gltf-binary", nil),
		"gltf_to_glb":           conversionAdapterSpec("gltf_to_glb", "glTF 模型", "gltf", "directory", "parent", nil, "glb", "file", ".glb", "model/gltf-binary", nil),
		"fbx_to_glb":            conversionAdapterSpec("fbx_to_glb", "FBX 模型", "fbx", "directory", "parent", nil, "glb", "file", ".glb", "model/gltf-binary", nil),
		"obj_to_glb":            conversionAdapterSpec("obj_to_glb", "OBJ 模型", "obj", "directory", "parent", nil, "glb", "file", ".glb", "model/gltf-binary", nil),
		"stl_to_glb":            conversionAdapterSpec("stl_to_glb", "STL 模型", "stl", "file", "file", nil, "glb", "file", ".glb", "model/gltf-binary", nil),
		"ifc_to_glb":            conversionAdapterSpec("ifc_to_glb", "IFC 模型", "ifc", "file", "file", nil, "glb", "file", ".glb", "model/gltf-binary", []commonModels.ParameterDescriptor{{Name: "center_model", Type: "boolean", Required: false, Default: true, Description: "转换时将 BIM 模型居中"}}),
		"osgb_scene_to_3dtiles": conversionAdapterSpec("osgb_scene_to_3dtiles", "OSGB Scene", "osgb_scene", "directory", "directory", nil, "3dtiles", "directory", "", "", nil),
		"gaussian_splat_to_ksplat": conversionAdapterSpec("gaussian_splat_to_ksplat", "Gaussian Splat", "ply", "file", "file", []string{"gaussian_splat"}, "ksplat", "file", ".ksplat", "application/vnd.gaussian-ksplat", []commonModels.ParameterDescriptor{
			{Name: "compression_level", Type: "integer", Required: false, Default: 1, Description: "KSplat 压缩等级"},
			{Name: "alpha_threshold", Type: "integer", Required: false, Default: 1, Description: "透明度阈值"},
			{Name: "spherical_harmonics_degree", Type: "integer", Required: false, Default: 0, Description: "球谐阶数"},
		}),
	}
}

func pointCloudWorkflowAdapterSpecs() map[string]workflowOperatorAdapterSpec {
	result := map[string]workflowOperatorAdapterSpec{}
	for _, item := range []struct{ id, label, format string }{
		{"las_to_copc", "LAS 点云", "las"},
		{"laz_to_copc", "LAZ 点云", "laz"},
		{"e57_to_copc", "E57 点云", "e57"},
		{"pcd_to_copc", "PCD 点云", "pcd"},
		{"xyz_to_copc", "XYZ 点云", "xyz"},
	} {
		result[item.id] = conversionAdapterSpec(item.id, item.label, item.format, "file", "file", []string{"point_cloud"}, "copc", "file", ".copc.laz", "application/vnd.laszip+copc", []commonModels.ParameterDescriptor{
			{Name: "threads", Type: "integer", Required: false, Default: 4, Description: "COPC 写入线程数"},
			{Name: "a_srs", Type: "string", Required: false, Description: "缺失时补充的源坐标参考系"},
		})
	}
	return result
}

func conversionAdapterSpec(
	operatorID, sourceLabel, sourceFormat, sourceKind, sourceScope string,
	sourceDataTypes []string,
	targetFormat, targetKind, targetExtension, contentType string,
	options []commonModels.ParameterDescriptor,
) workflowOperatorAdapterSpec {
	fileFormats := []string{sourceFormat}
	if operatorID == "gaussian_splat_to_ksplat" {
		fileFormats = []string{"ply", "splat"}
	}
	sourceUI := map[string]interface{}{
		"api_base_url":          "/api/v1/meta",
		"engine_families":       []string{"file", "object"},
		"selectable_node_types": []string{"file", "object", "directory"},
		"file_formats":          fileFormats,
		"resource_binding": map[string]interface{}{
			"mode":          "existing",
			"locator_param": "locator",
		},
	}
	if len(sourceDataTypes) > 0 {
		sourceUI["data_types"] = sourceDataTypes
	}
	public := []commonModels.ParameterDescriptor{
		resourcePickerParameter("数据源", "选择已有的"+sourceLabel+"数据项。", nil, sourceUI),
		resourceIdentityParameter("locator", "源数据项 ResourceLocator"),
		resourcePickerParameter("输出位置", "选择业务存储中的目标目录、Bucket 或 Prefix。", nil, map[string]interface{}{
			"api_base_url":                 "/api/v1/meta",
			"engine_families":              []string{"file", "object"},
			"selectable_parent_node_types": []string{"root", "directory", "dir", "bucket", "prefix"},
			"target_name_kind":             map[bool]string{true: "dataset", false: "file"}[targetKind == "directory"],
			"target_name_extension":        targetExtension,
			"resource_binding": map[string]interface{}{
				"mode":                 "target",
				"parent_locator_param": "target_parent_locator",
				"name_param":           "target_name",
				"default_params":       map[string]interface{}{"write_mode": "create"},
			},
		}),
		resourceIdentityParameter("target_parent_locator", "目标父节点 ResourceLocator"),
		resourceIdentityParameter("target_name", "目标名称"),
		{Name: "write_mode", Type: "string", Required: true, Default: "create", Enum: []string{"create", "replace"}, Description: "目标写入模式"},
	}
	public = append(public, options...)
	optionNames := make([]string, 0, len(options))
	for _, option := range options {
		optionNames = append(optionNames, option.Name)
	}
	return workflowOperatorAdapterSpec{
		OperatorID:       operatorID,
		PublicParameters: public,
		RuntimeParams:    []string{"access_plan", "options"},
		AccessPlan: &workflowAccessPlanSpec{
			SourceFormat: sourceFormat, SourceKind: sourceKind, SourceScope: sourceScope, SourceDataTypes: sourceDataTypes,
			TargetFormat: targetFormat, TargetKind: targetKind, TargetExtension: targetExtension, TargetContentType: contentType,
			OptionParams: optionNames,
		},
	}
}

func workflowSuperMapOpenPostgisAdapterSpec() workflowOperatorAdapterSpec {
	return workflowOperatorAdapterSpec{
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
			resourcePickerParameter(
				"输出位置",
				"选择已注册为 SuperMap SDX+ 的 PostGIS Schema 或数据库。",
				map[string]interface{}{"read_only": false},
				map[string]interface{}{
					"api_base_url":                 "/api/v1/meta",
					"engine_families":              []string{"tabular"},
					"selectable_parent_node_types": []string{"schema", "database"},
					"allow_create_table":           true,
					"require_spatial_workspace": map[string]interface{}{
						"ecosystem": "supermap",
						"kind":      "sdx+",
					},
					"resource_binding": map[string]interface{}{
						"mode":                 "target",
						"parent_locator_param": "target_parent_locator",
						"name_param":           "target_name",
						"default_params":       map[string]interface{}{"read_only": false},
					},
				},
			),
			resourceIdentityParameter("target_parent_locator", "目标父节点 ResourceLocator"),
			resourceIdentityParameter("target_name", "目标 Dataset 名称"),
		},
		ResourceInputs: []workflowResourceInputSpec{{
			PublicParam:   "locator",
			RuntimeParams: []string{"engine_id", "connection_info", "schema", "table"},
		}},
		ResourceOutputs: []workflowResourceOutputSpec{{
			ParentParam:   "target_parent_locator",
			NameParam:     "target_name",
			RuntimeParams: []string{"engine_id", "connection_info", "schema", "table"},
		}},
	}
}

func workflowSuperMapCreateUdbxAdapterSpec() workflowOperatorAdapterSpec {
	return workflowOperatorAdapterSpec{
		OperatorID: "datasource.create",
		PublicParameters: []commonModels.ParameterDescriptor{
			resourcePickerParameter(
				"UDBX 保存目录",
				"选择 NFS 目录保存 UDBX 成果文件。",
				nil,
				map[string]interface{}{
					"api_base_url":                 "/api/v1/meta",
					"engine_families":              []string{"file"},
					"engine_types":                 []string{"nfs"},
					"selectable_parent_node_types": []string{"root", "directory", "dir"},
					"resource_binding": map[string]interface{}{
						"mode":                 "target",
						"parent_locator_param": "target_parent_locator",
						"name_param":           "target_name",
						"default_params":       map[string]interface{}{"overwrite": false},
					},
				},
			),
			resourceIdentityParameter("target_parent_locator", "目标父节点 ResourceLocator"),
			resourceIdentityParameter("target_name", "UDBX 文件名"),
		},
		ResourceOutputs: []workflowResourceOutputSpec{{
			ParentParam:   "target_parent_locator",
			NameParam:     "target_name",
			RuntimeParams: []string{"engine_id", "connection_info", "path"},
		}},
	}
}

func workflowPythonSaveAdapterSpec() workflowOperatorAdapterSpec {
	return workflowOperatorAdapterSpec{
		OperatorID: "save",
		PublicParameters: []commonModels.ParameterDescriptor{
			resourcePickerParameter(
				"保存目标",
				"选择目标 Schema、数据库、目录、Bucket 或 Prefix。",
				nil,
				map[string]interface{}{
					"api_base_url":                 "/api/v1/meta",
					"engine_families":              []string{"tabular", "dynamic_schema", "file", "object"},
					"selectable_parent_node_types": []string{"schema", "database", "root", "directory", "dir", "bucket", "prefix"},
					"resource_binding": map[string]interface{}{
						"mode":                 "target",
						"parent_locator_param": "target_parent_locator",
						"name_param":           "target_name",
						"default_params":       map[string]interface{}{"mode": "replace"},
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
	result := map[string]struct{}{}
	if len(spec.ResourceInputs) > 0 || len(spec.ResourceOutputs) > 0 {
		result["engine_id"] = struct{}{}
		result["connection_info"] = struct{}{}
	}
	for _, param := range spec.RuntimeParams {
		result[param] = struct{}{}
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
