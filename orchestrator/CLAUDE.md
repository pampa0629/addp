# Orchestrator 模块说明

## 核心职责

Orchestrator 模块是 ADDP 平台的**工作流编排中枢**,负责以下核心功能:

1. **DAG 工作流编排** - 基于有向无环图 (DAG) 的多步骤任务编排,支持复杂的任务依赖关系
2. **任务调度与执行** - 通过拓扑排序自动解析任务依赖并按正确顺序执行,支持并发控制
3. **动态引擎调用** - 从 System 模块的能力注册中心动态加载计算引擎,通过统一的 TaskClient 调用各模块任务
4. **参数模板化** - 支持步骤间数据传递,通过 `{{stepID.field}}` 语法引用前序步骤的执行结果
5. **定时调度** - 基于 Cron 表达式的定时工作流执行,自动触发编排任务
6. **执行状态追踪** - 记录每次执行的详细信息,包括步骤结果、错误信息、执行时长等

## 关键架构

### 工作流编排架构

```
用户创建编排定义
  ↓
Orchestrator 存储到 PostgreSQL (orchestrations 表)
  ↓
手动触发 / 定时触发 (Scheduler)
  ↓
创建执行实例 (executions 表)
  ↓
Executor 执行编排
  ├─ 构建 DAG (buildDAG)
  ├─ 拓扑排序 (topologicalSort)
  ├─ 检测循环依赖
  └─ 逐步执行
     ↓
对于每个步骤:
  ├─ 判断执行模式
  │  ├─ 新架构: engine_identifier 动态引擎调用
  │  └─ 旧架构: module 硬编码模块调用 (向后兼容)
  ├─ 解析参数模板 (resolveTemplateReferences)
  │  └─ 替换 {{stepID.field}} 为前序步骤结果
  ├─ 执行步骤 (executeStep)
  │  ├─ 新架构: executeWithDynamicEngine
  │  │  ├─ EngineRegistry.GetEngine (从 System 查询引擎配置)
  │  │  ├─ TaskClient.CreateTask (调用引擎 create API)
  │  │  ├─ TaskClient.ExecuteTask (调用引擎 execute API)
  │  │  └─ pollTaskStatusDynamic (轮询任务状态,5 秒间隔)
  │  └─ 旧架构: executeWithModuleClient
  │     ├─ ModuleClient.Call (调用模块 API)
  │     └─ pollTaskStatus (轮询任务状态)
  ├─ 记录步骤结果 (StepResult)
  │  ├─ status: "success" / "failed"
  │  ├─ result: map[string]interface{} (执行结果)
  │  ├─ error: 错误信息
  │  ├─ duration: 执行时长 (毫秒)
  └─ 依赖步骤失败 → 中止编排
  ↓
更新执行状态 (completed / failed)
  ├─ StepResults: 所有步骤的执行结果
  ├─ ErrorMessage: 失败原因
  └─ CompletedAt: 完成时间
```

### 动态引擎调用架构 (新架构)

Orchestrator 通过 **能力注册中心** 实现模块解耦和动态扩展:

```
System 模块 (能力注册中心)
  ├─ engines 表 (存储引擎配置)
  │  ├─ unique_identifier: "meta.scanner.default"
  │  ├─ resource_type: "compute_engine"
  │  ├─ capabilities: JSON (能力声明)
  │  └─ task_api_config: JSON (API 配置)
  ↓
EngineRegistry (Orchestrator)
  ├─ 从 System 查询引擎配置
  ├─ 本地缓存 (5 分钟 TTL)
  └─ GetEngine(identifier) → Engine
  ↓
TaskClient (Orchestrator)
  ├─ 解析 TaskAPIConfig
  │  ├─ base_url: 引擎 API 基础 URL
  │  ├─ endpoints: create/execute/status API 配置
  │  ├─ body_template: 请求体模板 (Go template)
  │  └─ response_mapping: 响应字段映射
  ├─ CreateTask: POST {base_url}{create.path}
  ├─ ExecuteTask: POST {base_url}{execute.path}
  └─ GetTaskStatus: GET {base_url}{status.path}
  ↓
目标模块 (Meta/Transfer/Manager/GeoPandas)
  ├─ 接收任务创建请求
  ├─ 执行任务
  └─ 返回任务状态
```

**关键优势**:
- **模块解耦**: Orchestrator 无需硬编码各模块的 API 地址和参数格式
- **动态扩展**: 新增计算引擎只需在 System 模块注册,无需修改 Orchestrator 代码
- **统一接口**: 所有引擎通过统一的 TaskClient 调用,简化编排逻辑
- **灵活配置**: API 配置支持 Go template 语法,适应不同模块的 API 风格

### DAG 拓扑排序

Orchestrator 使用 **Kahn 算法** 对 DAG 进行拓扑排序,确保任务按依赖顺序执行:

```go
// 1. 计算所有步骤的入度
inDegree[step1] = 0  // 无依赖
inDegree[step2] = 1  // 依赖 step1
inDegree[step3] = 2  // 依赖 step1 和 step2

// 2. 找出所有入度为 0 的步骤加入队列
queue = [step1]

// 3. 逐个处理队列中的步骤
while queue 不为空:
    step = queue.pop()
    sorted.append(step)

    // 减少依赖该步骤的其他步骤的入度
    for dependent in step.dependents:
        inDegree[dependent]--
        if inDegree[dependent] == 0:
            queue.append(dependent)

// 4. 检查是否有环
if len(sorted) != len(steps):
    return error("检测到循环依赖")
```

**循环依赖检测**: 如果排序后的步骤数量少于总步骤数,说明存在循环依赖,编排执行会失败。

**示例**:
```json
// 编排定义
{
  "steps": [
    {"id": "A", "depends_on": []},
    {"id": "B", "depends_on": ["A"]},
    {"id": "C", "depends_on": ["A"]},
    {"id": "D", "depends_on": ["B", "C"]}
  ]
}

// 拓扑排序结果: A → B → C → D (或 A → C → B → D)
```

### 参数模板化机制

Orchestrator 支持通过 `{{stepID.field}}` 语法实现步骤间数据传递:

```go
// 步骤定义
{
  "id": "step2",
  "parameters": {
    "input_file": "{{step1.output_path}}",
    "config": {
      "threshold": "{{step1.statistics.max_value}}"
    }
  },
  "depends_on": ["step1"]
}

// 执行过程
// 1. step1 执行完成,结果存储到 stepResults
stepResults["step1"] = {
  "status": "success",
  "result": {
    "output_path": "s3://bucket/data.csv",
    "statistics": {
      "max_value": 100
    }
  }
}

// 2. 执行 step2 前,解析模板引用
resolvedParams = {
  "input_file": "s3://bucket/data.csv",  // 从 step1.result.output_path 提取
  "config": {
    "threshold": 100  // 从 step1.result.statistics.max_value 提取
  }
}

// 3. 使用解析后的参数调用 TaskClient
TaskClient.ExecuteTask(engine, taskID, resolvedParams)
```

**支持的数据类型**:
- 字符串: `"{{step1.name}}"` → `"北京"`
- 数字: `{{step1.count}}` → `100`
- 对象: `{{step1.config}}` → `{"key": "value"}`
- 数组: `{{step1.items}}` → `[1, 2, 3]`
- 嵌套字段: `{{step1.metadata.author.name}}` → `"张三"`

### 定时调度架构

```
Scheduler 启动
  ↓
加载所有启用的编排 (enabled=true)
  ↓
解析 Schedule (Cron 表达式)
  ├─ 示例: "0 2 * * *" → 每天凌晨 2 点
  ├─ 示例: "*/5 * * * *" → 每 5 分钟
  └─ 示例: "0 0 * * 0" → 每周日凌晨
  ↓
注册到 common/scheduler (基于 robfig/cron)
  ├─ 创建 TaskHandler (触发器)
  │  └─ 创建 Execution 实例
  │  └─ 调用 Executor.ExecuteAsync()
  └─ 监听 Cron 触发事件
  ↓
定时触发 → 创建 Execution → 异步执行编排
```

**调度管理**:
- 启用编排: `Schedule(orchID, cronExpr)` - 注册到调度器
- 禁用编排: `Unschedule(orchID)` - 从调度器移除
- 手动触发: 直接创建 Execution 并调用 `ExecuteAsync()`

### 依赖的其他模块

- **System 模块** (`common/client/system.go`) - 获取计算引擎配置 (EngineRegistry)、任务提供者配置 (TaskProviderRegistry)
- **Redis** - 缓存引擎配置、任务状态 (可选)
- **PostgreSQL** - 存储编排定义、执行记录

**模块间调用**:
```
Orchestrator
  ↓ (通过 TaskClient 调用)
Meta 模块 (元数据扫描)
Transfer 模块 (数据传输)
Manager 模块 (MVT 瓦片生成)
GeoPandas 引擎 (空间分析)
```

### 使用的中间件资源

- **PostgreSQL Schema**: `orchestrator`
  - `orchestrations` 表 (编排定义)
  - `executions` 表 (执行实例)
- **Redis Key 前缀** (可选):
  - `orchestrator:cache:engine:{id}` - 引擎缓存
  - `orchestrator:cache:provider:{name}` - 任务提供者缓存
- **MinIO Bucket**: 无 (Orchestrator 不存储数据)

## 重要文件位置

### 核心服务文件

- [executor.go](backend/internal/service/executor.go) - **编排执行引擎**（DAG 解析、拓扑排序、步骤执行）
- [scheduler.go](backend/internal/service/scheduler.go) - **定时调度器**（基于 Cron 的定时触发）
- [engine_registry.go](backend/internal/service/engine_registry.go) - **引擎注册表**（从 System 动态加载计算引擎配置）
- [task_provider_registry.go](backend/internal/service/task_provider_registry.go) - **任务提供者注册表**（从 System 动态加载任务提供者）
- [task_client.go](backend/internal/service/task_client.go) - **通用任务客户端**（动态调用各模块的任务 API）
- [module_client.go](backend/internal/service/module_client.go) - **模块客户端**（旧架构,硬编码模块调用,向后兼容）

### 数据模型文件

- [orchestration.go](backend/internal/models/orchestration.go) - 编排定义模型（Orchestration, Step, Execution, StepResult）

### API 路由文件

- [backend/internal/api/router.go](backend/internal/api/router.go) - HTTP 路由定义
- [backend/internal/api/handler.go](backend/internal/api/handler.go) - API 处理器（创建/更新/删除编排、触发执行、查询状态）

### 前端视图文件

- [frontend/src/views/OrchestrationList.vue](frontend/src/views/OrchestrationList.vue) - 编排列表界面
- [frontend/src/views/OrchestrationForm.vue](frontend/src/views/OrchestrationForm.vue) - 编排创建/编辑表单
- [frontend/src/views/ExecutionList.vue](frontend/src/views/ExecutionList.vue) - 执行历史列表
- [frontend/src/views/ExecutionRecords.vue](frontend/src/views/ExecutionRecords.vue) - 执行详情（步骤结果、错误信息）

### 配置文件

- [backend/internal/config/config.go](backend/internal/config/config.go) - 配置加载逻辑
- [.env](../.env) - 环境变量（`ORCHESTRATOR_*` 前缀）

### 文档文件

- [docs/DATA_STRUCTURES.md](docs/DATA_STRUCTURES.md) - 数据结构和 API 文档
- [docs/PARAMETER_TEMPLATING.md](docs/PARAMETER_TEMPLATING.md) - 参数模板化功能说明

## 常见开发场景

### 场景 1: 创建一个简单的工作流

**需求示例**: 创建一个工作流,先扫描数据库元数据,然后生成 MVT 瓦片

**步骤**:

1. **定义编排 JSON**:
   ```json
   {
     "name": "元数据扫描 + MVT 生成",
     "description": "自动扫描数据库并生成空间数据瓦片",
     "steps": [
       {
         "id": "scan_metadata",
         "name": "扫描元数据",
         "engine_identifier": "meta.scanner.default",
         "parameters": {
           "engine_id": 1,
           "scan_type": "full",
           "schema_names": ["public"]
         },
         "depends_on": [],
         "timeout": 300
       },
       {
         "id": "generate_mvt",
         "name": "生成 MVT 瓦片",
         "engine_identifier": "manager.mvt.default",
         "parameters": {
           "engine_id": 1,
           "schema": "public",
           "table": "cities",
           "zoom_levels": [0, 1, 2, 3, 4, 5]
         },
         "depends_on": ["scan_metadata"],
         "timeout": 600
       }
     ],
     "enabled": false
   }
   ```

2. **通过 API 创建编排**:
   ```bash
   curl -X POST http://localhost:8084/api/v1/orchestrations \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d @orchestration.json
   ```

3. **手动触发执行**:
   ```bash
   curl -X POST http://localhost:8084/api/v1/orchestrations/<id>/execute \
     -H "Authorization: Bearer <token>"
   ```

4. **查看执行结果**:
   ```bash
   # 查看所有执行记录
   curl -H "Authorization: Bearer <token>" \
     "http://localhost:8084/api/v1/executions?orchestration_id=<id>"

   # 查看单次执行详情（包含步骤结果）
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8084/api/v1/executions/<execution_id>
   ```

**相关文件**:
- [handler.go:CreateOrchestration](backend/internal/api/handler.go) - 创建编排 API
- [handler.go:ExecuteOrchestration](backend/internal/api/handler.go) - 触发执行 API
- [executor.go:executeSync](backend/internal/service/executor.go) - 同步执行逻辑

### 场景 2: 使用参数模板化实现数据流转

**需求示例**: 先从数据库提取数据,然后进行空间分析,最后导出结果

**步骤**:

1. **定义带参数模板的编排**:
   ```json
   {
     "name": "数据提取 → 空间分析 → 结果导出",
     "steps": [
       {
         "id": "extract_data",
         "name": "SQL 数据提取",
         "engine_identifier": "sql.postgresql.default",
         "parameters": {
           "query": "SELECT geom, name FROM poi WHERE city='北京'",
           "engine_id": 5
         },
         "depends_on": [],
         "timeout": 300
       },
       {
         "id": "spatial_analysis",
         "name": "空间缓冲区分析",
         "engine_identifier": "python-workflow.engine.default",
         "parameters": {
           "task_id": 1,
           "inputs": {
             "poi_geojson": "{{extract_data.geojson}}",
             "buffer_distance": 0.001
           }
         },
         "depends_on": ["extract_data"],
         "timeout": 600
       },
       {
         "id": "export_result",
         "name": "导出为 Shapefile",
         "engine_identifier": "transfer.worker.default",
         "parameters": {
           "task_type": "export",
           "source_geojson": "{{spatial_analysis.result_geojson}}",
           "target_path": "s3://bucket/beijing-poi-buffer.shp",
           "format": "shapefile"
         },
         "depends_on": ["spatial_analysis"],
         "timeout": 300
       }
     ]
   }
   ```

2. **触发执行**:
   ```bash
   curl -X POST http://localhost:8084/api/v1/orchestrations/<id>/execute \
     -H "Authorization: Bearer <token>"
   ```

3. **验证参数解析**（查看执行日志）:
   ```bash
   tail -f logs/orchestrator-backend.log | grep "参数模板"
   # 输出示例:
   # [INFO] 参数模板解析: {{extract_data.geojson}} → s3://bucket/temp/data.geojson
   # [INFO] 参数模板解析: {{spatial_analysis.result_geojson}} → s3://bucket/temp/buffer.geojson
   ```

**关键实现**:
- [executor.go:resolveTemplateReferences](backend/internal/service/executor.go) - 参数模板解析入口
- [executor.go:resolveStringTemplate](backend/internal/service/executor.go) - 字符串模板解析逻辑
- [executor.go:resolveValue](backend/internal/service/executor.go) - 递归解析嵌套结构

**相关文档**: [docs/PARAMETER_TEMPLATING.md](docs/PARAMETER_TEMPLATING.md)

### 场景 3: 配置定时工作流

**需求示例**: 每天凌晨 2 点自动执行元数据扫描和 MVT 生成

**步骤**:

1. **创建编排时指定 Schedule**:
   ```json
   {
     "name": "每日元数据更新",
     "steps": [...],
     "enabled": true,
     "schedule": "0 2 * * *"  // Cron 表达式: 每天凌晨 2 点
   }
   ```

2. **提交编排**:
   ```bash
   curl -X POST http://localhost:8084/api/v1/orchestrations \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d @scheduled-orchestration.json
   ```

3. **验证调度注册**（查看启动日志）:
   ```bash
   grep "调度器" logs/orchestrator-backend.log
   # 输出示例:
   # [INFO] 调度器启动成功
   # [INFO] 已注册编排调度: id=1, schedule=0 2 * * *
   ```

4. **查看下次执行时间**:
   ```bash
   curl -H "Authorization: Bearer <token>" \
     http://localhost:8084/api/v1/orchestrations/<id>
   # 返回的 next_run_at 字段显示下次执行时间
   ```

**常用 Cron 表达式**:
- `0 2 * * *` - 每天凌晨 2 点
- `*/5 * * * *` - 每 5 分钟
- `0 0 * * 0` - 每周日凌晨
- `0 0 1 * *` - 每月 1 日凌晨

**相关文件**:
- [scheduler.go:Start](backend/internal/service/scheduler.go) - 启动调度器,加载编排
- [scheduler.go:Schedule](backend/internal/service/scheduler.go) - 注册单个编排到调度器
- [scheduler.go:triggerOrchestration](backend/internal/service/scheduler.go) - 定时触发逻辑

### 场景 4: 调试工作流执行失败

**常见错误类型**:

1. **"检测到循环依赖"** → 步骤的 `depends_on` 形成环路
2. **"no engine found"** → `engine_identifier` 不存在或未注册到 System
3. **"failed to create task"** → 目标模块的 create API 调用失败
4. **"轮询超时"** → 任务执行时间超过 `timeout` 设置

**调试步骤**:

```bash
# 1. 查看 Orchestrator 后端日志
tail -f logs/orchestrator-backend.log

# 2. 查看执行详情（包含每个步骤的错误信息）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8084/api/v1/executions/<execution_id> | jq

# 关键字段:
# - status: "running" / "completed" / "failed"
# - current_step: 当前执行到的步骤 ID
# - step_results: 每个步骤的执行结果和错误
# - error_message: 整体错误信息

# 3. 检查步骤结果
curl -H "Authorization: Bearer <token>" \
  http://localhost:8084/api/v1/executions/<execution_id> | jq '.step_results'

# 输出示例:
# {
#   "step1": {
#     "status": "success",
#     "result": {...},
#     "duration": 5234
#   },
#   "step2": {
#     "status": "failed",
#     "error": "failed to create task: HTTP 500",
#     "duration": 1023
#   }
# }

# 4. 检查引擎配置（如果提示 engine not found）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8080/api/v1/engines?resource_type=compute_engine

# 5. 手动测试目标模块 API（如果任务创建失败）
# 例如测试 Meta 模块的扫描 API
curl -X POST http://localhost:8082/api/v1/manual-scan \
  -H "Authorization: Bearer <token>" \
  -d '{"engine_id": 1, "scan_type": "full"}'
```

**常见问题排查**:

| 错误信息 | 可能原因 | 解决方法 |
|---------|---------|---------|
| `检测到循环依赖` | DAG 中存在环路 | 检查 `depends_on` 字段,确保无循环引用 |
| `no engine found` | 引擎未注册 | 检查 System 模块的 engines 表,确认 `unique_identifier` 正确 |
| `failed to parse task_api_config` | TaskAPIConfig JSON 格式错误 | 检查 engines 表的 `task_api_config` 字段格式 |
| `轮询超时` | 任务执行时间过长 | 增加 `timeout` 参数或优化目标任务逻辑 |
| `HTTP 404` | API 路径错误 | 检查 TaskAPIConfig 的 `endpoints.*.path` 配置 |

**相关文件**:
- [executor.go:executeSync](backend/internal/service/executor.go) - 执行入口和错误处理
- [executor.go:markFailed](backend/internal/service/executor.go) - 失败标记逻辑
- [task_client.go:CreateTask](backend/internal/service/task_client.go) - 任务创建和错误处理

### 场景 5: 添加新的计算引擎支持

**需求示例**: 集成一个新的空间分析引擎 "CustomGIS"

**步骤**:

1. **在 System 模块注册引擎** (无需修改 Orchestrator 代码):
   ```bash
   curl -X POST http://localhost:8080/api/v1/engines \
     -H "Authorization: Bearer <token>" \
     -H "Content-Type: application/json" \
     -d '{
       "name": "CustomGIS 空间分析引擎",
       "resource_type": "compute_engine",
       "unique_identifier": "customgis.engine.default",
       "connection_info": {
         "type": "http",
         "base_url": "http://customgis-service:8090"
       },
       "capabilities": {
         "compute": [
           {
             "type": "spatial_analysis",
             "operations": ["buffer", "intersect", "union"]
           }
         ]
       },
       "task_api_config": {
         "base_url": "http://customgis-service:8090",
         "endpoints": {
           "create": {
             "method": "POST",
             "path": "/api/v1/tasks",
             "body_template": {
               "operation": "{{.Operation}}",
               "inputs": "{{.Inputs}}"
             }
           },
           "execute": {
             "method": "POST",
             "path": "/api/v1/tasks/{{.TaskID}}/execute",
             "body_template": null
           },
           "status": {
             "method": "GET",
             "path": "/api/v1/tasks/{{.TaskID}}/status",
             "response_mapping": {
               "status_field": "state",
               "message_field": "error",
               "progress_field": "percent"
             }
           }
         },
         "timeout": {
           "create": 30,
           "execute": 300,
           "status": 10
         }
       }
     }'
   ```

2. **在编排中使用新引擎**:
   ```json
   {
     "steps": [
       {
         "id": "custom_gis_analysis",
         "name": "CustomGIS 缓冲区分析",
         "engine_identifier": "customgis.engine.default",
         "parameters": {
           "operation": "buffer",
           "inputs": {
             "geometry": "{{extract_data.geojson}}",
             "distance": 100
           }
         },
         "depends_on": ["extract_data"],
         "timeout": 600
       }
     ]
   }
   ```

3. **测试执行**:
   ```bash
   curl -X POST http://localhost:8084/api/v1/orchestrations/<id>/execute \
     -H "Authorization: Bearer <token>"
   ```

4. **验证引擎调用** (查看日志):
   ```bash
   tail -f logs/orchestrator-backend.log | grep "customgis"
   # 输出示例:
   # [INFO] 引擎配置加载: customgis.engine.default
   # [INFO] 任务创建: POST http://customgis-service:8090/api/v1/tasks
   # [INFO] 任务执行: POST http://customgis-service:8090/api/v1/tasks/123/execute
   ```

**关键设计**:
- Orchestrator 无需修改代码,所有引擎配置通过 System 模块动态加载
- TaskClient 根据 `task_api_config` 自动适配不同引擎的 API 风格
- 参数模板支持灵活的数据传递

**相关文件**:
- [engine_registry.go:GetEngine](backend/internal/service/engine_registry.go) - 从 System 查询引擎配置
- [task_client.go:CreateTask](backend/internal/service/task_client.go) - 根据 TaskAPIConfig 调用 create API
- [task_client.go:ExecuteTask](backend/internal/service/task_client.go) - 根据 TaskAPIConfig 调用 execute API

## 注意事项

### 1. DAG 循环依赖检测

Orchestrator 使用拓扑排序算法,会自动检测循环依赖:

```json
// ❌ 错误: 循环依赖
{
  "steps": [
    {"id": "A", "depends_on": ["B"]},
    {"id": "B", "depends_on": ["A"]}
  ]
}
// 执行时会报错: "检测到循环依赖"

// ✅ 正确: 无环依赖
{
  "steps": [
    {"id": "A", "depends_on": []},
    {"id": "B", "depends_on": ["A"]},
    {"id": "C", "depends_on": ["A", "B"]}
  ]
}
```

**重要**: 创建编排前,务必检查 `depends_on` 字段是否形成有向无环图 (DAG)。

### 2. 参数模板引用规则

参数模板遵循以下规则:

- **步骤顺序**: 只能引用 `depends_on` 中的前序步骤结果,不能引用后续步骤
- **字段路径**: 使用 `.` 分隔符访问嵌套字段,如 `{{step1.result.nested.field}}`
- **类型安全**: 如果字段不存在或类型不匹配,返回 `nil`
- **非模板字符串**: 不包含 `{{}}` 的字符串原样传递

**错误示例**:
```json
// ❌ 错误: 引用未依赖的步骤
{
  "id": "step2",
  "parameters": {
    "data": "{{step3.result}}"  // step3 不在 depends_on 中
  },
  "depends_on": ["step1"]
}

// ✅ 正确: 只引用依赖的步骤
{
  "id": "step2",
  "parameters": {
    "data": "{{step1.result}}"
  },
  "depends_on": ["step1"]
}
```

### 3. 引擎配置缓存策略

EngineRegistry 使用本地缓存 (默认 5 分钟 TTL):

- **缓存命中**: 直接从内存返回引擎配置,无需调用 System 模块
- **缓存未命中**: 从 System 查询引擎配置并更新缓存
- **缓存过期**: 超过 TTL 后重新从 System 加载

**陷阱**: 如果在 System 模块更新了引擎配置,Orchestrator 最多需要 5 分钟才能生效。可手动清除缓存:

```bash
# 重启 Orchestrator 服务强制刷新缓存
bash scripts/dev/restart.sh -orchestrator
```

### 4. 任务执行超时控制

每个步骤支持单独的超时控制:

```json
{
  "id": "long_running_task",
  "timeout": 3600  // 超时 3600 秒 (1 小时)
}
```

**超时行为**:
- 超过 `timeout` 秒后,Orchestrator 停止轮询任务状态
- 步骤标记为 `failed`,整个编排中止
- **注意**: 目标模块的任务可能仍在执行,Orchestrator 只是停止等待

**默认超时**: 如果未指定 `timeout`,默认 5 分钟 (300 秒)。

### 5. 异步执行模型

Orchestrator 采用 **Go 协程 + 轮询** 的异步执行模型:

```go
// 触发执行 API
ExecuteOrchestration() {
    execution := createExecution()
    executor.ExecuteAsync(execution.ID)  // 立即返回
    return {execution_id: execution.ID}
}

// 异步执行逻辑
ExecuteAsync(executionID) {
    go func() {
        executeSync(executionID)  // 在 goroutine 中执行
    }()
}
```

**影响**:
- 调用 `/execute` API 立即返回,不阻塞
- 前端需要轮询 `/executions/{id}` 查询执行状态
- 长时间运行的工作流不会占用 HTTP 连接

### 6. 新旧架构兼容性

Orchestrator 同时支持新旧两种执行模式:

**新架构** (推荐):
```json
{
  "engine_identifier": "meta.scanner.default"  // 动态引擎调用
}
```

**旧架构** (向后兼容):
```json
{
  "module": "meta",           // 硬编码模块名
  "endpoint": "/api/v1/scan",
  "method": "POST"
}
```

**判断逻辑** (在 Executor 中):
```go
if step.EngineIdentifier != "" {
    // 使用新架构: 动态引擎调用
    executeWithDynamicEngine(step)
} else if step.Module != "" {
    // 使用旧架构: 硬编码模块调用
    executeWithModuleClient(step)
}
```

**迁移建议**: 优先使用新架构 (engine_identifier),旧架构仅用于向后兼容。

## 典型开发工作流

### 修改 Orchestrator 后端代码后

```bash
# 1. 重启 Orchestrator 后端服务（会自动重新编译）
bash scripts/dev/restart.sh -orchestrator

# 2. 查看启动日志（确认编译成功）
tail -f logs/orchestrator-backend.log

# 3. 测试 API（使用 Portal 登录获取 token）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8084/api/v1/orchestrations
```

### 添加新的引擎支持后

```bash
# 1. 在 System 模块注册引擎（参考场景 5）
curl -X POST http://localhost:8080/api/v1/engines \
  -H "Authorization: Bearer <token>" \
  -d @new-engine.json

# 2. 验证引擎注册
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/engines?resource_type=compute_engine"

# 3. 在编排中使用新引擎
curl -X POST http://localhost:8084/api/v1/orchestrations \
  -H "Authorization: Bearer <token>" \
  -d @orchestration-with-new-engine.json

# 4. 触发执行测试
curl -X POST http://localhost:8084/api/v1/orchestrations/<id>/execute \
  -H "Authorization: Bearer <token>"

# 5. 查看执行日志
tail -f logs/orchestrator-backend.log | grep "新引擎"
```

### 调试工作流执行失败

```bash
# 1. 查看执行详情
curl -H "Authorization: Bearer <token>" \
  http://localhost:8084/api/v1/executions/<execution_id> | jq

# 2. 查看失败步骤的错误信息
curl -H "Authorization: Bearer <token>" \
  http://localhost:8084/api/v1/executions/<execution_id> | jq '.step_results'

# 3. 查看 Orchestrator 后端日志（搜索关键词）
grep "execution_id=<execution_id>" logs/orchestrator-backend.log

# 4. 检查引擎配置（如果提示 engine not found）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8080/api/v1/engines/<engine_id>

# 5. 手动测试目标模块 API（复现错误）
curl -X POST http://<module_url>/api/v1/tasks \
  -H "Authorization: Bearer <token>" \
  -d '{"参数": "值"}'
```

## 相关文档

- **数据结构和 API 文档** - [docs/DATA_STRUCTURES.md](docs/DATA_STRUCTURES.md)
- **参数模板化功能说明** - [docs/PARAMETER_TEMPLATING.md](docs/PARAMETER_TEMPLATING.md)
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **Meta 模块说明** - [meta/CLAUDE.md](../meta/CLAUDE.md)
- **Transfer 模块说明** - [transfer/CLAUDE.md](../transfer/CLAUDE.md)（如果有）
- **Manager 模块说明** - [manager/CLAUDE.md](../manager/CLAUDE.md)
