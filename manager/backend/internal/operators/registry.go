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
		Inputs:  []string{"spatial_layer"},
		Outputs: []string{"tile_cache"},
	},
}
