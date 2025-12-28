package operators

import "github.com/addp/common/models"

// float64Ptr 辅助函数: 返回float64指针
func float64Ptr(f float64) *float64 {
	return &f
}

// MetaOperators Meta模块算子列表
var MetaOperators = []models.OperatorMetadata{
	{
		ID:          "scan_basic",
		Name:        "scan_basic",
		DisplayName: "基础扫描",
		Type:        "scan",
		Category:    "元数据管理",
		Description: "快速扫描资源的基础元数据(目录结构、文件列表)",
		Module:      "meta",
		Parameters: []models.ParameterMetadata{
			{
				Name:        "engine_id",
				Type:        "integer",
				Required:    true,
				Description: "要扫描的资源ID",
				Min:         float64Ptr(1),
			},
			{
				Name:        "schema_names",
				Type:        "array",
				Required:    false,
				Description: "指定扫描的Schema列表(数据库类型资源)",
				ItemType:    "string",
			},
			{
				Name:        "object_paths",
				Type:        "array",
				Required:    false,
				Description: "指定扫描的对象路径(对象存储类型资源)",
				ItemType:    "string",
			},
		},
		Inputs:  []string{"resource"},
		OutputPorts: []models.OutputPortMetadata{
		{
			Name:        "default",
			Type:        "metadata",
			Description: "算子输出",
			IsDefault:   true,
		},
	},
	},
	{
		ID:          "scan_deep",
		Name:        "scan_deep",
		DisplayName: "深度扫描",
		Type:        "scan",
		Category:    "元数据管理",
		Description: "深度扫描资源的完整元数据(包含文件内容、字段详情、统计信息)",
		Module:      "meta",
		Parameters: []models.ParameterMetadata{
			{
				Name:        "engine_id",
				Type:        "integer",
				Required:    true,
				Description: "要扫描的资源ID",
			},
			{
				Name:        "schema_names",
				Type:        "array",
				Required:    false,
				ItemType:    "string",
				Description: "指定扫描的Schema列表",
			},
			{
				Name:        "object_paths",
				Type:        "array",
				Required:    false,
				ItemType:    "string",
				Description: "指定扫描的对象路径",
			},
		},
		Inputs:  []string{"resource"},
		OutputPorts: []models.OutputPortMetadata{
		{
			Name:        "default",
			Type:        "metadata",
			Description: "算子输出",
			IsDefault:   true,
		},
	},
	},
}
