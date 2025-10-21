# Transfer 模块编译错误说明

## 📊 当前状态

| 组件 | 状态 | 说明 |
|------|------|------|
| Models | ✅ 正常 | 数据模型定义完整 |
| Pipeline | ✅ 正常 | 数据管道核心逻辑正常 |
| Connector | ✅ 已修复 | 数据连接器（File, JDBC, S3）已修复 |
| Repository | ✅ 正常 | 数据访问层正常 |
| Service | ⚠️ 部分完成 | 核心功能正常，缺少部分 API 方法 |
| API Handler | ❌ 不完整 | 与 Service 层接口不匹配 |

## ✅ 已修复的编译错误

### 1. task_service.go - StartTime 字段类型错误
```go
// 修复前
StartTime: nil  // ❌ cannot use nil as time.Time value

// 修复后  
StartTime: time.Now()  // ✅ 正确
```

### 2. connector/utils.go - 缺失工具函数
新增了配置读取辅助函数：
- `getStringConfig()` - 读取字符串配置
- `getIntConfig()` - 读取整数配置  
- `getBoolConfig()` - 读取布尔配置

### 3. ConnectorConfig 结构体初始化错误
```go
// 修复前
pipeline.ConnectorConfig{
    "file_path": path,  // ❌ invalid field name
}

// 修复后
pipeline.ConnectorConfig{
    Config: map[string]interface{}{
        "file_path": path,  // ✅ 正确
    },
    BatchSize: 1000,
}
```

### 4. Schema 结构体字段错误
```go
// 修复前
&pipeline.Schema{
    Name: filename,  // ❌ unknown field 'Name'
    Fields: fields,
}

// 修复后
&pipeline.Schema{
    Fields: fields,
    Metadata: map[string]interface{}{
        "source_file": filename,  // ✅ 使用 Metadata
    },
}
```

### 5. JDBC connector 注册方式错误
```go
// 修复前
registry.RegisterReader("jdbc", func(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
    reader := NewJDBCReader()  // ❌ 缺少参数
    return reader, nil
})

// 修复后  
registry.RegisterReader("jdbc", NewJDBCReader)  // ✅ 直接传递函数
```

## ❌ 剩余的编译错误（API 层）

### execution_handler.go

```go
// 错误 1: ListExecutions 参数不匹配
h.executionService.ListExecutions(ctx, tenantID, filters, page, pageSize)
// ❌ cannot use filters (map[string]interface{}) as uint

// 错误 2: 方法不存在
h.executionService.GetTaskExecutions(ctx, taskID, tenantID)
// ❌ undefined: GetTaskExecutions

// 错误 3: GetExecutionLogs 参数过多
h.executionService.GetExecutionLogs(ctx, executionID, tenantID, limit)
// ❌ 期望: (ctx, executionID, tenantID)

// 错误 4: 方法不存在
h.executionService.GetExecutionStatistics(ctx, tenantID)
// ❌ undefined: GetExecutionStatistics
```

### task_handler.go

```go
// 错误 1: ListTasks 参数不匹配
h.taskService.ListTasks(ctx, tenantID, filters, page, pageSize)
// ❌ 期望: (ctx, tenantID, *models.ListTasksRequest)

// 错误 2: UpdateTask 参数顺序错误
h.taskService.UpdateTask(ctx, id, req, tenantID, userID)
// ❌ 期望: (ctx, tenantID, id, req)

// 错误 3: 方法不存在
h.taskService.GetStatistics(ctx, tenantID)
// ❌ undefined: GetStatistics

h.taskService.CreateMapping(...)
// ❌ undefined: CreateMapping

h.taskService.GetTaskMappings(...)
// ❌ undefined: GetTaskMappings

// 错误 4: 类型未定义
var req models.CreateDataMappingRequest
// ❌ undefined: CreateDataMappingRequest
```

## 🔧 需要完成的工作

### 1. Service 层 - ExecutionService

需要实现以下方法：

```go
type ExecutionService struct {
    // 现有字段...
}

// 需要添加的方法：
func (s *ExecutionService) ListExecutions(ctx context.Context, tenantID uint, req *models.ListExecutionsRequest) (*models.PaginatedExecutions, error)
func (s *ExecutionService) GetTaskExecutions(ctx context.Context, taskID uint, tenantID uint) ([]*models.TaskExecution, error)
func (s *ExecutionService) GetExecutionLogs(ctx context.Context, executionID uint, tenantID uint) (string, error)
func (s *ExecutionService) GetExecutionStatistics(ctx context.Context, tenantID uint) (*models.ExecutionStatistics, error)
```

### 2. Service 层 - TaskService

需要实现以下方法：

```go
type TaskService struct {
    // 现有字段...
}

// 需要添加的方法：
func (s *TaskService) GetStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error)
func (s *TaskService) CreateMapping(ctx context.Context, taskID uint, req *models.CreateDataMappingRequest) (*models.DataMapping, error)
func (s *TaskService) GetTaskMappings(ctx context.Context, taskID uint) ([]*models.DataMapping, error)
func (s *TaskService) UpdateMapping(ctx context.Context, mappingID uint, req *models.UpdateDataMappingRequest) error
func (s *TaskService) DeleteMapping(ctx context.Context, mappingID uint) error
```

### 3. Models 层

需要添加以下数据模型：

```go
// ListExecutionsRequest 执行记录查询请求
type ListExecutionsRequest struct {
    TaskID   *uint           `form:"task_id"`
    Status   *ExecutionStatus `form:"status"`
    Page     int             `form:"page"`
    PageSize int             `form:"page_size"`
}

// CreateDataMappingRequest 创建映射请求
type CreateDataMappingRequest struct {
    SourceField  string `json:"source_field" binding:"required"`
    TargetField  string `json:"target_field" binding:"required"`
    Transform    string `json:"transform"`
    DefaultValue string `json:"default_value"`
    FieldType    string `json:"field_type"`
    Format       string `json:"format"`
    Nullable     bool   `json:"nullable"`
}

// UpdateDataMappingRequest 更新映射请求
type UpdateDataMappingRequest struct {
    SourceField  *string `json:"source_field"`
    TargetField  *string `json:"target_field"`
    Transform    *string `json:"transform"`
    DefaultValue *string `json:"default_value"`
    FieldType    *string `json:"field_type"`
    Format       *string `json:"format"`
    Nullable     *bool   `json:"nullable"`
}

// ExecutionStatistics 执行统计
type ExecutionStatistics struct {
    TotalExecutions   int64 `json:"total_executions"`
    SuccessExecutions int64 `json:"success_executions"`
    FailedExecutions  int64 `json:"failed_executions"`
    RunningExecutions int64 `json:"running_executions"`
    TotalRecords      int64 `json:"total_records"`
    TotalBytes        int64 `json:"total_bytes"`
}

// PaginatedExecutions 分页执行记录
type PaginatedExecutions struct {
    Data       []*TaskExecution `json:"data"`
    Total      int64            `json:"total"`
    Page       int              `json:"page"`
    PageSize   int              `json:"page_size"`
}
```

### 4. API Handler 调整

需要调整方法调用以匹配 Service 层接口：

```go
// task_handler.go
tasks, err := h.taskService.ListTasks(ctx, tenantID, &models.ListTasksRequest{
    Type:     req.Type,
    Status:   req.Status,
    Page:     req.Page,
    PageSize: req.PageSize,
})

err := h.taskService.UpdateTask(ctx, tenantID, id, req)
```

## 🎯 推荐的修复顺序

1. **Models 层** - 添加缺失的数据模型（最简单）
2. **Service 层** - 实现缺失的方法（参考 Manager/Meta 模块）
3. **API Handler** - 调整方法调用以匹配 Service 接口（最后一步）

## 📚 参考资料

可以参考以下模块的实现：

- **Manager 模块**: [manager/backend/internal/service/](../../manager/backend/internal/service/)
- **Meta 模块**: [meta/backend/internal/service/](../../meta/backend/internal/service/)
- **System 模块**: [system/backend/internal/service/](../../system/backend/internal/service/)

## 🚀 临时解决方案

在 API 层完善之前，Transfer 模块暂时不启动。核心功能（connector, pipeline）已修复并可用，可以通过以下方式测试：

```go
// 直接使用 connector 和 pipeline
reader := connector.NewFileReader()
config := pipeline.ConnectorConfig{
    Config: map[string]interface{}{
        "file_path": "/path/to/file.csv",
        "file_type": "csv",
    },
}
reader.Open(ctx, config)
batch, _ := reader.Read(ctx)
```

## 📝 更新日志

- 2025-01-21: 修复 connector 层所有编译错误
- 2025-01-21: 识别 API 层与 Service 层接口不匹配问题
- 2025-01-21: 暂时跳过 Transfer 模块启动，等待 API 层完善
