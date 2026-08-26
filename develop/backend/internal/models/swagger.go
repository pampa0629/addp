package models

import (
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
)

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error     string `json:"error" example:"请求参数错误"`
	ErrorCode string `json:"error_code,omitempty" example:"invalid_query_parameter_definitions"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Code    int         `json:"code" example:"200"`
	Message string      `json:"message" example:"操作成功"`
	Data    interface{} `json:"data,omitempty"`
}

// DevTaskContentSwagger 开发任务内容 Swagger 摘要。
// 实际运行时仍由 DevTaskService 按 dev_type 做强校验；此结构用于让 Swagger 展示 query/workflow/script 的规范字段。
type DevTaskContentSwagger struct {
	Query              string                     `json:"query,omitempty" example:"SELECT * FROM cities"`
	QueryType          string                     `json:"query_type,omitempty" enums:"sql,mql,cypher" example:"sql"`
	TargetLocator      string                     `json:"target_locator,omitempty" example:"addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657"`
	QueryParameters    []QueryParameterSwagger    `json:"query_parameters,omitempty"`
	WorkflowDefinition *WorkflowDefinitionSwagger `json:"workflow_definition,omitempty"`
	Inputs             map[string]interface{}     `json:"inputs,omitempty" swaggertype:"object"`
	NotebookPath       string                     `json:"notebook_path,omitempty" example:"demo.ipynb"`
	Parameters         map[string]interface{}     `json:"parameters,omitempty" swaggertype:"object"`
}

// QueryParameterSwagger 查询值参数定义；default 的 JSON 标量类型必须与 type 一致。
type QueryParameterSwagger struct {
	Name        string      `json:"name" example:"city_name"`
	Type        string      `json:"type" enums:"string,integer,number,boolean" example:"string"`
	Default     interface{} `json:"default" swaggertype:"primitive,string" example:"Beijing"`
	Title       string      `json:"title,omitempty" example:"城市名称"`
	Description string      `json:"description,omitempty" example:"用于筛选目标城市"`
}

// WorkflowDefinitionSwagger addp.workflow/v1 工作流定义。
type WorkflowDefinitionSwagger struct {
	Tasks []WorkflowTaskDefinitionSwagger `json:"tasks" binding:"required"`
}

// WorkflowTaskDefinitionSwagger 工作流任务定义。
type WorkflowTaskDefinitionSwagger struct {
	ID        string                 `json:"id" binding:"required" example:"load_1"`
	Operator  string                 `json:"operator" binding:"required" example:"load"`
	Params    map[string]interface{} `json:"params" binding:"required" swaggertype:"object"`
	DependsOn []string               `json:"depends_on" binding:"required" example:"load_1"`
}

// DevTaskExecutionConfigSwagger 开发任务执行配置 Swagger 摘要。
type DevTaskExecutionConfigSwagger struct {
	Type                  string                               `json:"type,omitempty" example:"workflow"`
	EngineID              uint                                 `json:"engine_id,omitempty" example:"12"`
	EngineSpecific        *WorkflowEngineSpecificConfigSwagger `json:"engine_specific,omitempty"`
	MaterializationTarget *MaterializationTargetSwagger        `json:"materialization_target,omitempty"`
}

// MaterializationTargetSwagger 将已保存的 SQL 查询静态绑定到 Model 逻辑表。
type MaterializationTargetSwagger struct {
	LogicalTableID int64 `json:"logical_table_id" example:"3"`
}

// WorkflowEngineSpecificConfigSwagger 工作流引擎特定执行配置。
type WorkflowEngineSpecificConfigSwagger struct {
	SparkClusterID uint `json:"spark_cluster_id,omitempty" example:"34"`
}

// DAGEditorLayoutSwagger DAG 编辑器展示状态，不参与运行时执行。
type DAGEditorLayoutSwagger struct {
	Nodes    map[string]DAGEditorNodePositionSwagger `json:"nodes"`
	Viewport DAGEditorViewportSwagger                `json:"viewport"`
}

// DAGEditorNodePositionSwagger DAG 节点坐标。
type DAGEditorNodePositionSwagger struct {
	X float64 `json:"x" example:"120"`
	Y float64 `json:"y" example:"240"`
}

// DAGEditorViewportSwagger DAG 画布视口。
type DAGEditorViewportSwagger struct {
	Zoom       float64 `json:"zoom" example:"1"`
	TranslateX float64 `json:"translate_x" example:"0"`
	TranslateY float64 `json:"translate_y" example:"0"`
}

// CreateDevTaskSwaggerRequest 创建开发任务 Swagger 请求体。
type CreateDevTaskSwaggerRequest struct {
	Name            string                        `json:"name" binding:"required" example:"city_buffer_workflow"`
	DisplayName     string                        `json:"display_name,omitempty" example:"城市缓冲区工作流"`
	DevType         string                        `json:"dev_type" binding:"required" enums:"query,workflow,script" example:"workflow"`
	Content         DevTaskContentSwagger         `json:"content" binding:"required"`
	ExecutionConfig DevTaskExecutionConfigSwagger `json:"execution_config,omitempty"`
	EditorLayout    DAGEditorLayoutSwagger        `json:"editor_layout,omitempty"`
	Timeout         int                           `json:"timeout,omitempty" example:"300"`
	Description     string                        `json:"description,omitempty" example:"示例工作流"`
	Tags            []string                      `json:"tags,omitempty"`
}

// UpdateDevTaskSwaggerRequest 更新开发任务 Swagger 请求体。
type UpdateDevTaskSwaggerRequest struct {
	Name            string                        `json:"name,omitempty" example:"city_buffer_workflow"`
	DisplayName     string                        `json:"display_name,omitempty" example:"城市缓冲区工作流"`
	Content         DevTaskContentSwagger         `json:"content,omitempty"`
	ExecutionConfig DevTaskExecutionConfigSwagger `json:"execution_config,omitempty"`
	EditorLayout    DAGEditorLayoutSwagger        `json:"editor_layout,omitempty"`
	Timeout         int                           `json:"timeout,omitempty" example:"300"`
	Description     string                        `json:"description,omitempty" example:"示例工作流"`
	Tags            []string                      `json:"tags,omitempty"`
	Status          string                        `json:"status,omitempty" enums:"active,inactive,archived" example:"active"`
}

// NotebookRuntimeBindingSwaggerRequest Notebook 运行时绑定请求体。
type NotebookRuntimeBindingSwaggerRequest struct {
	EngineID uint   `json:"engine_id" binding:"required" example:"10"`
	Kernel   string `json:"kernel" binding:"required" example:"python3"`
}

// RebindWorkflowStorageEngineSwaggerRequest 工作流存储引擎重绑定请求体。
type RebindWorkflowStorageEngineSwaggerRequest struct {
	TargetEngineID uint `json:"target_engine_id" binding:"required" example:"15"`
}

// StorageEngineDescriptorSwagger 存储引擎脱敏候选摘要。
type StorageEngineDescriptorSwagger struct {
	ID               uint   `json:"id" example:"15"`
	Name             string `json:"name" example:"Business Doris"`
	EngineType       string `json:"engine_type" example:"doris"`
	LifecycleState   string `json:"lifecycle_state" example:"active"`
	ConnectionStatus string `json:"connection_status" example:"connected"`
}

// WorkflowStorageEngineBindingSwagger 工作流中的一个存储引擎引用集合。
type WorkflowStorageEngineBindingSwagger struct {
	EngineID            uint                            `json:"engine_id" example:"5"`
	ReferenceCount      int                             `json:"reference_count" example:"2"`
	ResourceTypes       []string                        `json:"resource_types" example:"table,database"`
	Available           bool                            `json:"available" example:"false"`
	Engine              *StorageEngineDescriptorSwagger `json:"engine,omitempty"`
	CompatibleEngineIDs []uint                          `json:"compatible_engine_ids" example:"2,15"`
}

// WorkflowStorageEngineBindingsSwaggerResponse 工作流存储引擎绑定列表。
type WorkflowStorageEngineBindingsSwaggerResponse struct {
	Items            []WorkflowStorageEngineBindingSwagger `json:"items"`
	CandidateEngines []StorageEngineDescriptorSwagger      `json:"candidate_engines"`
}

// RebindWorkflowStorageEngineSwaggerResponse 工作流存储引擎重绑定结果。
type RebindWorkflowStorageEngineSwaggerResponse struct {
	Task                 DevTaskSwagger `json:"task"`
	SourceEngineID       uint           `json:"source_engine_id" example:"5"`
	TargetEngineID       uint           `json:"target_engine_id" example:"15"`
	ReplacedLocatorCount int            `json:"replaced_locator_count" example:"2"`
}

// DevTaskSwagger 开发任务 Swagger 响应摘要。
type DevTaskSwagger struct {
	ID                  uint                          `json:"id" example:"1"`
	TenantID            uint                          `json:"tenant_id" example:"1"`
	Name                string                        `json:"name" example:"city_buffer_workflow"`
	DisplayName         string                        `json:"display_name,omitempty" example:"城市缓冲区工作流"`
	DevType             string                        `json:"dev_type" enums:"query,workflow,script" example:"workflow"`
	Content             DevTaskContentSwagger         `json:"content"`
	ExecutionConfig     DevTaskExecutionConfigSwagger `json:"execution_config,omitempty"`
	EditorLayout        DAGEditorLayoutSwagger        `json:"editor_layout"`
	Parameters          map[string]interface{}        `json:"parameters,omitempty" swaggertype:"object"`
	Timeout             int                           `json:"timeout" example:"300"`
	Description         string                        `json:"description,omitempty" example:"示例工作流"`
	Tags                []string                      `json:"tags,omitempty"`
	CreatedBy           *uint                         `json:"created_by,omitempty" example:"1"`
	UpdatedBy           *uint                         `json:"updated_by,omitempty" example:"1"`
	CreatedAt           string                        `json:"created_at" example:"2026-06-24T12:00:00Z"`
	UpdatedAt           string                        `json:"updated_at" example:"2026-06-24T12:00:00Z"`
	Status              string                        `json:"status" enums:"active,inactive,archived" example:"active"`
	LastExecutionID     *string                       `json:"last_execution_id,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	LastExecutionStatus string                        `json:"last_execution_status,omitempty" example:"success"`
	LastRunAt           string                        `json:"last_run_at,omitempty" example:"2026-06-24T12:00:00Z"`
}

// DevTaskDetailSwagger 开发任务详情，包含本次执行可覆盖的任务级参数契约。
type DevTaskDetailSwagger struct {
	DevTaskSwagger
	ExecutionContract taskprovider.ExecutionContract `json:"execution_contract"`
}

// ProviderDevTaskSwagger TaskProvider 标准开发任务 Swagger 响应摘要。
type ProviderDevTaskSwagger struct {
	DevTaskSwagger
	TaskType          string                         `json:"task_type" enums:"query,workflow,script" example:"workflow"`
	ExecutionContract taskprovider.ExecutionContract `json:"execution_contract"`
}

// ListDevTasksSwaggerResponse 开发任务列表 Swagger 响应。
type ListDevTasksSwaggerResponse struct {
	Items    []DevTaskSwagger `json:"items"`
	Total    int64            `json:"total" example:"1"`
	Page     int              `json:"page" example:"1"`
	PageSize int              `json:"page_size" example:"20"`
}

// ListProviderDevTasksSwaggerResponse TaskProvider 标准任务列表 Swagger 响应。
type ListProviderDevTasksSwaggerResponse struct {
	Items    []ProviderDevTaskSwagger `json:"items"`
	Total    int64                    `json:"total" example:"1"`
	Page     int                      `json:"page" example:"1"`
	PageSize int                      `json:"page_size" example:"20"`
}

// UploadNotebookSwaggerResponse 上传 Notebook Swagger 响应。
type UploadNotebookSwaggerResponse struct {
	Message string         `json:"message" example:"Notebook 上传成功"`
	DevTask DevTaskSwagger `json:"dev_task"`
}

// CreateNotebookSwaggerRequest 新建空白 Notebook 请求。
type CreateNotebookSwaggerRequest struct {
	Name        string `json:"name" binding:"required" example:"analysis"`
	Description string `json:"description,omitempty" example:"Exploratory analysis"`
	EngineID    uint   `json:"engine_id" binding:"required" example:"10"`
	Kernel      string `json:"kernel" binding:"required" example:"python3"`
}

// NotebookSessionSwaggerResponse 浏览器可见的 Notebook 交互会话事实。
type NotebookSessionSwaggerResponse struct {
	ID        string `json:"id" format:"uuid" example:"550e8400-e29b-41d4-a716-446655440000"`
	TaskID    uint   `json:"task_id" example:"12"`
	URL       string `json:"url" example:"/api/v1/develop/notebook-sessions/550e8400-e29b-41d4-a716-446655440000/lab/tree/analysis.ipynb"`
	ExpiresAt string `json:"expires_at" format:"date-time" example:"2026-08-02T12:00:00Z"`
}

// TaskExecutionSwagger 统一执行记录 Swagger 响应摘要。
type TaskExecutionSwagger struct {
	ID                int64                  `json:"id" example:"1"`
	TenantID          int                    `json:"tenant_id" example:"1"`
	ExecutionID       string                 `json:"execution_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Module            string                 `json:"module" example:"develop"`
	TaskType          string                 `json:"task_type" enums:"query,workflow,script" example:"workflow"`
	Source            string                 `json:"source" example:"develop"`
	SourceTaskID      *string                `json:"source_task_id,omitempty" example:"12"`
	SourceTaskName    *string                `json:"source_task_name,omitempty" example:"city_buffer_workflow"`
	ParentExecutionID *string                `json:"parent_execution_id,omitempty" example:"parent-execution-id"`
	Status            string                 `json:"status" enums:"pending,running,success,failed,timeout,cancelled" example:"success"`
	Progress          int                    `json:"progress" example:"100"`
	CurrentStep       *string                `json:"current_step,omitempty" example:"执行工作流"`
	TriggerType       string                 `json:"trigger_type" enums:"manual,scheduled,event" example:"manual"`
	TriggeredBy       *int                   `json:"triggered_by,omitempty" example:"1"`
	ExecutionConfig   map[string]interface{} `json:"execution_config,omitempty" swaggertype:"object"`
	ErrorDetails      map[string]interface{} `json:"error_details,omitempty" swaggertype:"object"`
	Metadata          map[string]interface{} `json:"metadata,omitempty" swaggertype:"object"`
	ExecutionTimeMs   *int64                 `json:"execution_time_ms,omitempty" example:"1200"`
	RowsAffected      *int64                 `json:"rows_affected,omitempty" example:"42"`
	RecordsRead       *int64                 `json:"records_read,omitempty" example:"100"`
	RecordsWritten    *int64                 `json:"records_written,omitempty" example:"100"`
	BytesRead         *int64                 `json:"bytes_read,omitempty" example:"1024"`
	BytesWritten      *int64                 `json:"bytes_written,omitempty" example:"2048"`
	StartedAt         string                 `json:"started_at,omitempty" example:"2026-06-24T12:00:00Z"`
	CompletedAt       string                 `json:"completed_at,omitempty" example:"2026-06-24T12:00:02Z"`
	CreatedAt         string                 `json:"created_at" example:"2026-06-24T12:00:00Z"`
	UpdatedAt         string                 `json:"updated_at" example:"2026-06-24T12:00:02Z"`
}

// ExecutionWithDevTaskSwagger 执行记录和开发任务关联 Swagger 响应。
type ExecutionWithDevTaskSwagger struct {
	TaskExecutionSwagger
	DevTask *DevTaskSwagger      `json:"dev_task,omitempty"`
	Outputs commonModels.JSONMap `json:"outputs,omitempty"`
}

// ListExecutionsSwaggerResponse 执行列表 Swagger 响应。
type ListExecutionsSwaggerResponse struct {
	Executions []ExecutionWithDevTaskSwagger `json:"executions"`
	Total      int64                         `json:"total" example:"1"`
	Page       int                           `json:"page" example:"1"`
	PageSize   int                           `json:"page_size" example:"20"`
}

// CreateExecutionSwaggerRequest 创建临时执行 Swagger 请求体。
type CreateExecutionSwaggerRequest struct {
	DevType                string                        `json:"dev_type,omitempty" enums:"query,workflow,script" example:"workflow"`
	TriggerType            string                        `json:"trigger_type,omitempty" enums:"manual,scheduled" example:"manual"`
	Content                DevTaskContentSwagger         `json:"content,omitempty"`
	ExecutionConfig        DevTaskExecutionConfigSwagger `json:"execution_config,omitempty"`
	Parameters             map[string]interface{}        `json:"parameters,omitempty" swaggertype:"object"`
	Timeout                int                           `json:"timeout,omitempty" example:"300"`
	QueryConfirmationToken string                        `json:"query_confirmation_token,omitempty" example:"signed-short-lived-token"`
	ApprovalID             string                        `json:"approval_id,omitempty" format:"uuid"`
	RequestFingerprint     string                        `json:"request_fingerprint,omitempty" example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
}
