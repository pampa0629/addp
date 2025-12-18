package operators

import "github.com/addp/common/models"

// float64Ptr 辅助函数: 返回float64指针
func float64Ptr(f float64) *float64 {
	return &f
}

// TransferOperators Transfer模块算子列表
var TransferOperators = []models.OperatorMetadata{
	{
		ID:          "batch_transfer",
		Name:        "batch_transfer",
		DisplayName: "批量传输",
		Type:        "transfer.batch",
		Category:    "数据传输",
		Description: "批量模式数据传输,支持全量/增量同步",
		Module:      "transfer",
		Parameters: []models.ParameterMetadata{
			{
				Name:        "source_id",
				Type:        "integer",
				Required:    true,
				Description: "源数据源ID",
			},
			{
				Name:        "target_id",
				Type:        "integer",
				Required:    true,
				Description: "目标数据源ID",
			},
			{
				Name:        "task_type",
				Type:        "string",
				Required:    true,
				Description: "传输类型",
				Enum:        []string{"import", "export", "sync"},
				Default:     "sync",
			},
			{
				Name:        "batch_size",
				Type:        "integer",
				Required:    false,
				Description: "批处理大小",
				Default:     1000,
				Min:         float64Ptr(100),
				Max:         float64Ptr(10000),
			},
			{
				Name:        "max_parallelism",
				Type:        "integer",
				Required:    false,
				Description: "最大并行度",
				Default:     1,
				Min:         float64Ptr(1),
				Max:         float64Ptr(10),
			},
			{
				Name:        "config",
				Type:        "object",
				Required:    true,
				Description: "传输配置",
				Properties: map[string]models.ParameterMetadata{
					"source_table": {
						Name:        "source_table",
						Type:        "string",
						Required:    true,
						Description: "源表名",
					},
					"target_table": {
						Name:        "target_table",
						Type:        "string",
						Required:    true,
						Description: "目标表名",
					},
					"where_clause": {
						Name:        "where_clause",
						Type:        "string",
						Required:    false,
						Description: "过滤条件(SQL WHERE子句)",
					},
				},
			},
		},
		Inputs:  []string{"source_resource", "target_resource"},
		Outputs: []string{"execution_result"},
	},
	{
		ID:          "stream_transfer",
		Name:        "stream_transfer",
		DisplayName: "流式传输",
		Type:        "transfer.stream",
		Category:    "数据传输",
		Description: "流式模式数据传输,实时同步数据变更",
		Module:      "transfer",
		Parameters: []models.ParameterMetadata{
			{
				Name:        "source_id",
				Type:        "integer",
				Required:    true,
				Description: "源数据源ID",
			},
			{
				Name:        "target_id",
				Type:        "integer",
				Required:    true,
				Description: "目标数据源ID",
			},
			{
				Name:        "config",
				Type:        "object",
				Required:    true,
				Description: "传输配置",
			},
		},
		Inputs:  []string{"source_resource", "target_resource"},
		Outputs: []string{"execution_result"},
	},
}
