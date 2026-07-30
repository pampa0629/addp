package models

// OutputPortDescriptor 输出端口描述
type OutputPortDescriptor struct {
	Name        string `json:"name"`        // 端口名称 (如 "default", "large", "small")
	Type        string `json:"type"`        // 数据类型 (geodataframe, string, array等)
	Description string `json:"description"` // 端口语义说明
	IsDefault   bool   `json:"is_default"`  // 是否为默认端口 (单输出时为true)
}

// OperatorDescriptor 算子描述
type OperatorDescriptor struct {
	ID                  string                 `json:"id"`                             // 算子唯一标识 (如 "scan_deep")
	Name                string                 `json:"name"`                           // 算子名称 (同ID)
	DisplayName         string                 `json:"display_name"`                   // 中文显示名 (如 "深度扫描")
	EngineType          string                 `json:"engine_type"`                    // 所属扩展引擎类型 (如 acme_geo_workflow)
	Type                string                 `json:"type"`                           // 算子类型 (scan/transfer/spatial等)
	Category            string                 `json:"category"`                       // 算子分类/分组 (格式转换/空间分析)
	CategoryPath        []string               `json:"category_path"`                  // 多级分组目录；无多级目录时必须显式为 [category]
	Description         string                 `json:"description"`                    // 功能描述
	BriefDescription    string                 `json:"brief_description,omitempty"`    // 简要描述（用于 AI 算子选择）
	DetailedDescription map[string]interface{} `json:"detailed_description,omitempty"` // 详细描述（含 notes、workflow_example 等，用于 AI 工作流生成）
	Parameters          []ParameterDescriptor  `json:"parameters"`                     // 参数定义
	Inputs              []string               `json:"inputs"`                         // 输入类型列表
	OutputPorts         []OutputPortDescriptor `json:"output_ports"`                   // 标准输出端口定义
	ExecutionModes      []string               `json:"execution_modes"`                // workflow/direct
	Effects             []string               `json:"effects"`                        // read/write/ddl/external_effect
	Attributes          map[string]interface{} `json:"attributes,omitempty"`           // 引擎自定义扩展属性
}

// ParameterDescriptor 参数描述
type ParameterDescriptor struct {
	Name        string                         `json:"name"`                 // 参数名
	Type        string                         `json:"type"`                 // 类型 (string/integer/float/boolean/array/object)
	ParamType   string                         `json:"param_type,omitempty"` // 参数角色 (input/output/param/ui)
	Required    bool                           `json:"required"`             // 是否必填
	Default     interface{}                    `json:"default,omitempty"`    // 默认值
	Description string                         `json:"description"`          // 参数说明
	Enum        []string                       `json:"enum,omitempty"`       // 枚举值 (用于下拉选择)
	Min         *float64                       `json:"min,omitempty"`        // 最小值 (数值类型)
	Max         *float64                       `json:"max,omitempty"`        // 最大值 (数值类型)
	Pattern     string                         `json:"pattern,omitempty"`    // 正则校验 (字符串类型)
	ItemType    string                         `json:"item_type,omitempty"`  // 数组元素类型
	Properties  map[string]ParameterDescriptor `json:"properties,omitempty"` // 对象属性定义
	DependsOn   string                         `json:"depends_on,omitempty"` // 依赖的参数名 (动态显示)
	ShowWhen    map[string]interface{}         `json:"show_when,omitempty"`  // 显示条件 (格式: {param_name: value_or_list})
	Notes       string                         `json:"notes,omitempty"`      // 注意事项/额外说明
	UIType      string                         `json:"ui_type,omitempty"`    // UI 组件类型 (resource_tree_picker 等)
	UIConfig    map[string]interface{}         `json:"ui_config,omitempty"`  // UI 组件配置参数
}

// OperatorsResponse 表达算子列表返回格式。
// 工作流引擎通过 WorkflowRuntimeProvider.ListOperators() 获取，
// 当前 addp.workflow/v1 HTTP 入口为 GET /api/operators。
type OperatorsResponse struct {
	Status    string               `json:"status"`    // "success"
	Operators []OperatorDescriptor `json:"operators"` // 算子列表
	Count     int                  `json:"count"`     // 算子总数
}

// OperatorInvokeRequest 表达单算子 direct 调用请求格式。
// 当前 addp.workflow/v1 入口为 POST /api/operators/{name}/invoke。
type OperatorInvokeRequest struct {
	Params  map[string]interface{} `json:"params" binding:"required"` // 算子参数
	Runtime map[string]interface{} `json:"runtime,omitempty"`         // 执行期运行时绑定参数
}

// OperatorInvokeResponse 表达单算子 direct 调用响应格式。
type OperatorInvokeResponse struct {
	Status          string                 `json:"status"`                 // "success"/"failed"
	ExecutionID     string                 `json:"execution_id,omitempty"` // runtime 本地执行ID，不是 ADDP 任务ID
	Result          map[string]interface{} `json:"result,omitempty"`       // 调用结果
	Error           string                 `json:"error,omitempty"`        // 错误信息
	Message         string                 `json:"message,omitempty"`      // 提示信息
	ExecutionTimeMs *float64               `json:"execution_time_ms,omitempty"`
}
