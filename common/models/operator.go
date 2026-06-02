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
	Type                string                 `json:"type"`                           // 算子类型 (scan/transfer/spatial等)
	Category            string                 `json:"category"`                       // 分类 (元数据管理/数据传输/空间计算)
	Description         string                 `json:"description"`                    // 功能描述
	BriefDescription    string                 `json:"brief_description,omitempty"`    // 简要描述（用于 AI 算子选择）
	DetailedDescription map[string]interface{} `json:"detailed_description,omitempty"` // 详细描述（含 notes、workflow_example 等，用于 AI 工作流生成）
	Parameters          []ParameterDescriptor  `json:"parameters"`                     // 参数定义
	Inputs              []string               `json:"inputs"`                         // 输入类型列表
	OutputPorts         []OutputPortDescriptor `json:"output_ports"`                   // 输出端口定义 (替代 Outputs)
	Module              string                 `json:"module"`                         // 所属模块 (meta/transfer/manager/python_workflow)
}

// ParameterDescriptor 参数描述
type ParameterDescriptor struct {
	Name        string                         `json:"name"`                 // 参数名
	Type        string                         `json:"type"`                 // 类型 (string/integer/float/boolean/array/object)
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
	UIType      string                         `json:"ui_type,omitempty"`    // UI 组件类型 (data_source_cascader/engine_select等)
	UIConfig    map[string]interface{}         `json:"ui_config,omitempty"`  // UI 组件配置参数
}

// OperatorsResponse GET /api/{module}/operators 返回格式
type OperatorsResponse struct {
	Status    string               `json:"status"`    // "success"
	Operators []OperatorDescriptor `json:"operators"` // 算子列表
	Count     int                  `json:"count"`     // 算子总数
}

// OperatorExecuteRequest POST /api/{module}/operators/:name/execute 请求格式
type OperatorExecuteRequest struct {
	Params     map[string]interface{} `json:"params" binding:"required"` // 算子参数
	ExecuteNow bool                   `json:"execute_now"`               // 是否立即执行(默认true)
	TaskName   string                 `json:"task_name,omitempty"`       // 任务名称(execute_now=false时)
}

// OperatorExecuteResponse POST /api/{module}/operators/:name/execute 响应格式
type OperatorExecuteResponse struct {
	Status     string                 `json:"status"`            // "success"/"error"
	TaskID     string                 `json:"task_id"`           // 任务ID
	TaskStatus string                 `json:"task_status"`       // "running"/"pending"/"completed"
	Result     map[string]interface{} `json:"result,omitempty"`  // 执行结果(同步执行时)
	Message    string                 `json:"message,omitempty"` // 提示信息
	CreatedAt  string                 `json:"created_at"`        // 创建时间
}
