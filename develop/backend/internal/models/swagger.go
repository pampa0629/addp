package models

// ErrorResponse 错误响应
type ErrorResponse struct {
	Code    int    `json:"code" example:"400"`
	Message string `json:"message" example:"请求参数错误"`
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
	QueryType          string                     `json:"query_type,omitempty" enums:"sql,mql,dsl" example:"sql"`
	WorkflowDefinition *WorkflowDefinitionSwagger `json:"workflow_definition,omitempty"`
	Inputs             map[string]interface{}     `json:"inputs,omitempty" swaggertype:"object"`
	NotebookPath       string                     `json:"notebook_path,omitempty" example:"demo.ipynb"`
	Parameters         map[string]interface{}     `json:"parameters,omitempty" swaggertype:"object"`
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
	Type           string                               `json:"type,omitempty" example:"workflow"`
	EngineID       uint                                 `json:"engine_id,omitempty" example:"12"`
	QueryMode      string                               `json:"query_mode,omitempty" enums:"duckdb" example:"duckdb"`
	EngineSpecific *WorkflowEngineSpecificConfigSwagger `json:"engine_specific,omitempty"`
}

// WorkflowEngineSpecificConfigSwagger 工作流引擎特定执行配置。
type WorkflowEngineSpecificConfigSwagger struct {
	SparkClusterID uint `json:"spark_cluster_id,omitempty" example:"34"`
}

// CreateDevTaskSwaggerRequest 创建开发任务 Swagger 请求体。
type CreateDevTaskSwaggerRequest struct {
	Name            string                        `json:"name" binding:"required" example:"city_buffer_workflow"`
	DisplayName     string                        `json:"display_name,omitempty" example:"城市缓冲区工作流"`
	DevType         string                        `json:"dev_type" binding:"required" enums:"query,workflow,script" example:"workflow"`
	Content         DevTaskContentSwagger         `json:"content" binding:"required"`
	ExecutionConfig DevTaskExecutionConfigSwagger `json:"execution_config,omitempty"`
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
	Timeout         int                           `json:"timeout,omitempty" example:"300"`
	Description     string                        `json:"description,omitempty" example:"示例工作流"`
	Tags            []string                      `json:"tags,omitempty"`
	Status          string                        `json:"status,omitempty" enums:"active,inactive,archived" example:"active"`
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

// ProviderDevTaskSwagger TaskProvider 标准开发任务 Swagger 响应摘要。
type ProviderDevTaskSwagger struct {
	DevTaskSwagger
	TaskType string `json:"task_type" enums:"query,workflow,script" example:"workflow"`
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
	DevTask *DevTaskSwagger `json:"dev_task,omitempty"`
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
	DevType         string                        `json:"dev_type" binding:"required" enums:"query,workflow,script" example:"workflow"`
	TriggerType     string                        `json:"trigger_type" binding:"required" enums:"manual,scheduled" example:"manual"`
	Content         DevTaskContentSwagger         `json:"content,omitempty"`
	ExecutionConfig DevTaskExecutionConfigSwagger `json:"execution_config" binding:"required"`
	Timeout         int                           `json:"timeout,omitempty" example:"300"`
}
