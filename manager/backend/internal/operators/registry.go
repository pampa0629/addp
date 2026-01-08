package operators

import "github.com/addp/common/models"

// float64Ptr 辅助函数: 返回float64指针
func float64Ptr(f float64) *float64 {
	return &f
}

// ManagerOperators Manager模块算子列表
var ManagerOperators = []models.OperatorMetadata{
	{
		ID:          "mvt_tile_cache",
		Name:        "mvt_tile_cache",
		DisplayName: "MVT瓦片缓存",
		Type:        "tile_cache",
		Category:    "数据管理",
		Description: "生成矢量瓦片(MVT)缓存,支持指定缩放级别范围",
		Module:      "manager",
		Parameters: []models.ParameterMetadata{
			{
				Name:        "layer_id",
				Type:        "integer",
				Required:    true,
				Description: "图层ID",
			},
			{
				Name:        "min_zoom",
				Type:        "integer",
				Required:    false,
				Description: "最小缩放级别",
				Default:     10,
				Min:         float64Ptr(0),
				Max:         float64Ptr(22),
			},
			{
				Name:        "max_zoom",
				Type:        "integer",
				Required:    false,
				Description: "最大缩放级别",
				Default:     16,
				Min:         float64Ptr(0),
				Max:         float64Ptr(22),
			},
			{
				Name:        "bbox",
				Type:        "array",
				Required:    false,
				Description: "边界框[minX, minY, maxX, maxY]",
				ItemType:    "float",
			},
		},
		Inputs: []string{"spatial_layer"},
		OutputPorts: []models.OutputPortMetadata{
			{
				Name:        "default",
				Type:        "tile_cache",
				Description: "算子输出",
				IsDefault:   true,
			},
		},
	},
	{
		ID:          "embedding",
		Name:        "embedding",
		DisplayName: "数据嵌入",
		Type:        "embedding",
		Category:    "数据管理",
		Description: "对对象存储中的文档、图片、视频进行向量化,支持单对象、目录和Bucket三级向量化",
		Module:      "manager",
		Parameters: []models.ParameterMetadata{
			{
				Name:        "engine_id",
				Type:        "integer",
				Required:    true,
				Description: "引擎ID(对象存储引擎)",
			},
			{
				Name:        "bucket",
				Type:        "string",
				Required:    true,
				Description: "存储桶名称",
			},
			{
				Name:        "object_key",
				Type:        "string",
				Required:    false,
				Description: "对象路径(单对象向量化时必填)",
			},
			{
				Name:        "prefix",
				Type:        "string",
				Required:    false,
				Description: "目录前缀(目录向量化时使用)",
			},
			{
				Name:        "recursive",
				Type:        "boolean",
				Required:    false,
				Description: "是否递归向量化子目录",
				Default:     true,
			},
			{
				Name:        "scope",
				Type:        "string",
				Required:    false,
				Description: "向量化范围: object(单对象), directory(目录), bucket(整个桶)",
				Default:     "object",
			},
		},
		Inputs: []string{"object_storage"},
		OutputPorts: []models.OutputPortMetadata{
			{
				Name:        "default",
				Type:        "embeddings",
				Description: "向量化结果",
				IsDefault:   true,
			},
		},
	},
}
