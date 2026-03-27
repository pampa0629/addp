# Copilot × Develop 调用关系说明

## 概述

Copilot 是一个**纯后端 API 服务**（Python FastAPI，端口 8087），没有独立前端。Develop 模块的工作流编辑器（`WorkflowEditor.vue`）内嵌了 AI 助手面板，用户在该面板中输入自然语言描述，前端将引擎类型等上下文随请求一起发给 Copilot，由 Copilot 后端驱动 LLM 完成工作流 DAG JSON 的生成，再返回给前端渲染到画布。

---

## 组件职责总览

| 层级 | 组件 | 职责 |
|------|------|------|
| **Develop 前端** | `WorkflowEditor.vue` | AI 助手面板 UI；用户输入自然语言、选择引擎类型；接收并渲染生成的工作流 DAG |
| **Develop 前端** | `OperatorPalette.vue` | 算子面板；按引擎类型加载可用算子列表供用户拖拽使用 |
| **Develop 前端** | `api/copilot.js` | 封装对 Copilot 后端的 HTTP 调用 |
| **Gateway** | `gateway:8000` | 统一路由入口；将 `/api/copilot/*` 反向代理到 Copilot 后端 |
| **Develop 后端** | `operator_discovery_service.go` | 算子发现服务；并发查询所有已注册的工作流引擎，聚合算子元数据，5 分钟 TTL 缓存 |
| **Copilot 后端** | `workflow_agent_api.py` | 工作流生成 API 端点；接收请求，调用 WorkflowPipeline |
| **Copilot 后端** | `workflow_pipeline.py` | 5 阶段流水线编排器；协调数据源理解、算子筛选、生成、验证全流程 |
| **Copilot 后端** | `operator_selection_chain.py` | LLM Chain；从全量算子列表中筛选 3-8 个最相关算子 |
| **Copilot 后端** | `workflow_generation_chain.py` | LLM Chain；根据选定算子的详情（含 workflow_example）生成完整 DAG JSON |
| **Copilot 后端** | `workflow_validation_chain.py` | 四层验证；结构、唯一性、依赖关系、参数合法性 |
| **Copilot 后端** | `develop_tools.py` | LangChain Tools；封装对 Develop 后端算子 API 的调用（发现 + 详情） |
| **工作流引擎** | `python-workflow` / `spark-workflow` / `math-workflow` | 暴露 `/api/operators` 端点；提供算子元数据（参数定义、输出定义、workflow_example） |
| **LLM 服务** | 通义千问 / OpenAI / Claude / Ollama | 执行算子筛选、工作流生成、自动修复等推理任务 |

---

## 时序图 1：Develop 前端发起工作流生成（主流程）

```mermaid
sequenceDiagram
    actor User as 用户
    participant FE as Develop前端<br/>WorkflowEditor.vue
    participant GW as Gateway<br/>:8000
    participant Copilot as Copilot后端<br/>:8087
    participant Pipeline as WorkflowPipeline
    participant DevBE as Develop后端<br/>:8084
    participant LLM as LLM服务

    User->>FE: 1. 在AI助手面板输入自然语言描述<br/>（已选择引擎类型：python_workflow）
    FE->>GW: 2. POST /api/copilot/workflow/generate<br/>{ query, engine_type, workflow_engine_id, tenant_id }
    GW->>Copilot: 3. 反向代理转发
    Copilot->>Pipeline: 4. run(query, engine_type, workflow_engine_id)

    note over Pipeline: 阶段1：数据源理解（可选）
    Pipeline->>LLM: 5. 分析查询中的数据源关键词
    LLM-->>Pipeline: 返回数据源上下文 DataSourceContext

    note over Pipeline: 阶段2：算子筛选
    Pipeline->>DevBE: 6. GET /api/develop/operators/modules/{engine_type}<br/>获取引擎的全量算子列表（简要信息）
    DevBE-->>Pipeline: 返回算子列表（name, brief, category）
    Pipeline->>LLM: 7. 从全量算子中筛选 3-8 个相关算子
    LLM-->>Pipeline: 返回选定算子名称列表

    note over Pipeline: 阶段3：工作流生成
    Pipeline->>DevBE: 8. 批量并发获取选定算子的详细信息<br/>GET /api/develop/operators/{name}
    DevBE-->>Pipeline: 返回算子详情（参数定义、outputs、workflow_example）
    Pipeline->>LLM: 9. 结合算子详情 + 数据源上下文<br/>生成工作流 DAG JSON
    LLM-->>Pipeline: 返回工作流 JSON

    note over Pipeline: 阶段4：验证 + 自动修复
    Pipeline->>Pipeline: 10. 四层验证<br/>（结构/唯一性/依赖/参数）
    alt 验证失败
        Pipeline->>LLM: 11. 自动修复（最多2次重试）
        LLM-->>Pipeline: 返回修复后的工作流
    end

    Pipeline-->>Copilot: 返回 WorkflowGenerationResponse
    Copilot-->>GW: { status, workflow, explanation, selected_operators, validation_result }
    GW-->>FE: 响应转发
    FE->>FE: 12. 将 workflow.tasks 渲染到 DAG 画布
    FE-->>User: 展示生成的工作流
```

---

## 时序图 2：算子发现机制（如何知道有哪些算子可用）

```mermaid
sequenceDiagram
    participant FE as Develop前端<br/>OperatorPalette.vue
    participant DevBE as Develop后端<br/>:8084
    participant DiscSvc as OperatorDiscoveryService<br/>（5分钟TTL缓存）
    participant PyEngine as Python工作流引擎<br/>python-workflow
    participant SparkEngine as Spark工作流引擎<br/>spark-workflow
    participant MathEngine as Math工作流引擎<br/>math-workflow
    participant Copilot as Copilot后端<br/>develop_tools.py

    note over FE,MathEngine: 路径A：Develop前端加载算子面板
    FE->>DevBE: GET /api/develop/operators/modules/{engine_type}<br/>（用户切换工作流引擎时触发）
    DevBE->>DiscSvc: GetOperatorsByModule(engine_type)

    alt 缓存未命中（冷启动或超过5分钟）
        DiscSvc->>PyEngine: GET /api/operators（并发）
        DiscSvc->>SparkEngine: GET /api/operators（并发）
        DiscSvc->>MathEngine: GET /api/operators（并发）
        PyEngine-->>DiscSvc: 返回 42 个算子元数据
        SparkEngine-->>DiscSvc: 返回 Spark 算子元数据
        MathEngine-->>DiscSvc: 返回 5 个数学算子元数据
        DiscSvc->>DiscSvc: 聚合所有算子，写入缓存
    end

    DiscSvc-->>DevBE: 返回指定引擎类型的算子列表
    DevBE-->>FE: 算子列表（按分类组织）
    FE->>FE: 渲染算子面板供用户拖拽使用

    note over Copilot,MathEngine: 路径B：Copilot生成工作流时获取算子
    Copilot->>DevBE: GET /api/develop/operators/modules/{engine_type}<br/>（OperatorDiscoveryTool，5分钟TTL缓存）
    DevBE->>DiscSvc: GetOperatorsByModule(engine_type)
    DiscSvc-->>DevBE: 返回缓存中的算子列表（简要信息）
    DevBE-->>Copilot: 算子列表（name, brief_description, category）

    Copilot->>DevBE: GET /api/develop/operators/{operator_name}<br/>（OperatorDetailTool，批量并发获取选定算子详情）
    DevBE-->>Copilot: 算子详情（parameters, outputs, workflow_example）
```

---

## 时序图 3：Copilot 内部工作流生成详细流程

```mermaid
sequenceDiagram
    participant API as workflow_agent_api.py
    participant Pipeline as WorkflowPipeline
    participant DS as DataSourceStage
    participant Sel as OperatorSelectionChain
    participant Gen as WorkflowGenerationChain
    participant Val as WorkflowValidationChain
    participant Fix as WorkflowAutoFix
    participant DevTools as develop_tools.py
    participant LLM as LLM服务

    API->>Pipeline: run(query, engine_type, workflow_engine_id)

    rect rgb(240,248,255)
        note right of Pipeline: 阶段1 数据源理解（可选）
        Pipeline->>DS: understand(query)
        DS->>LLM: 提取数据源关键信息
        LLM-->>DS: 数据源关键词
        DS->>DevTools: EngineTool → 获取所有存储引擎
        DevTools-->>DS: 引擎列表
        DS->>LLM: 匹配最合适的引擎
        LLM-->>DS: DataSourceContext（engine_id, location, confidence）
        DS-->>Pipeline: DataSourceContext
    end

    rect rgb(240,255,240)
        note right of Pipeline: 阶段2 算子筛选
        Pipeline->>Sel: select(query, data_source, engine_type)
        Sel->>DevTools: OperatorDiscoveryTool → 获取算子列表
        DevTools-->>Sel: 算子列表（简要信息）
        Sel->>LLM: 从全量算子中筛选 3-8 个<br/>（附带分类和简介）
        LLM-->>Sel: 选定算子名称列表
        Sel-->>Pipeline: ["load", "buffer", "save"] 等
    end

    rect rgb(255,248,240)
        note right of Pipeline: 阶段3 工作流生成
        Pipeline->>Gen: generate(query, data_source, operators, engine_type)
        Gen->>DevTools: OperatorDetailTool（并发批量）<br/>获取每个选定算子的详情
        DevTools-->>Gen: 算子详情（parameters, outputs, workflow_example）
        Gen->>LLM: 生成工作流 DAG<br/>（参考 workflow_example 确保参数格式正确）
        LLM-->>Gen: 工作流 JSON 字符串
        Gen->>Gen: 清理 markdown 标记、解析 JSON
        Gen-->>Pipeline: Workflow 对象
    end

    rect rgb(255,240,255)
        note right of Pipeline: 阶段4 验证 + 自动修复
        Pipeline->>Val: validate(workflow, operator_details)
        Val->>Val: 结构验证（tasks字段、必需字段）
        Val->>Val: 唯一性验证（task ID 唯一）
        Val->>Val: 依赖验证（Kahn 算法检测循环依赖）
        Val->>Val: 参数验证（算子存在性、必需参数、引用格式）
        Val-->>Pipeline: ValidationResult

        alt 验证失败 && 重试次数 < 2
            Pipeline->>Fix: auto_fix(workflow, errors)
            Fix->>LLM: 根据错误提示修复工作流
            LLM-->>Fix: 修复后的工作流 JSON
            Fix-->>Pipeline: 修复后的 Workflow
            Pipeline->>Val: 重新验证
        end
    end

    Pipeline-->>API: WorkflowGenerationResponse<br/>{ status, workflow, explanation, selected_operators, validation_result }
```

---

## 工作流引擎选择机制

工作流引擎的选择发生在 **Develop 前端**，用户在 `WorkflowEditor.vue` 中选择引擎类型后，该信息作为参数随 Copilot API 请求传递，贯穿整个生成流程。

### 三种引擎对比

| 引擎类型 | 参数值 | 适用场景 | 算子数量 |
|---------|--------|---------|---------|
| Python 工作流 | `python_workflow` | 空间数据处理（GIS 操作）、中小规模数据 | 42 个（24 空间 + 18 非空间） |
| Spark 工作流 | `spark_workflow` | 大规模分布式数据处理 | 若干（含空间分析、SQL 查询等） |
| Math 工作流 | `math_workflow` | 基础数学运算演示/测试 | 5 个（加减乘除、平均） |

### 引擎 ID 的作用

除了 `engine_type`（引擎类型），请求还携带 `workflow_engine_id`（引擎实例 ID）。两者作用不同：

- `engine_type`：决定**用哪类引擎**的算子，Copilot 据此获取对应的算子元数据
- `workflow_engine_id`：指向系统中注册的**具体引擎实例**（如某个 Python 运行时服务），用于数据源匹配和最终执行

### 参数传递路径

```
Develop前端（用户选择引擎）
  → POST /api/copilot/workflow/generate
    { engine_type: "python_workflow", workflow_engine_id: 1 }
  → Copilot WorkflowPipeline
    → OperatorDiscoveryTool: GET /api/develop/operators/modules/python_workflow
    → 算子筛选 → 工作流生成（算子均来自 python_workflow 命名空间）
  → 返回工作流 JSON（包含算子名称，均为 python_workflow 引擎支持的算子）
  → 工作流执行时使用 workflow_engine_id 指定执行实例
```

---

## 算子元数据结构

每个算子向 Develop 后端暴露标准化的元数据，其中 `workflow_example` 是 LLM 生成工作流时最重要的参考字段。

```json
{
  "id": "buffer",
  "name": "buffer",
  "display_name": "缓冲区分析",
  "category": "空间分析",
  "module": "python_workflow",
  "brief_description": "对几何对象生成指定距离的缓冲区",
  "detailed_description": "...",
  "parameters": [
    {
      "name": "distance",
      "type": "float",
      "required": true,
      "description": "缓冲区距离",
      "notes": "单位由 unit 参数决定"
    },
    {
      "name": "unit",
      "type": "string",
      "required": false,
      "default": "meters",
      "enum": ["meters", "kilometers", "degrees"]
    }
  ],
  "outputs": [
    {
      "name": "result",
      "type": "GeoDataFrame",
      "description": "缓冲区结果"
    }
  ],
  "workflow_example": {
    "id": "buffer_task",
    "operator": "buffer",
    "params": {
      "distance": 100,
      "unit": "meters"
    },
    "depends_on": ["load_task"]
  },
  "use_cases": ["POI 服务范围分析", "洪水淹没区域计算"],
  "notes": ["输入几何必须已设置正确的坐标系"]
}
```

### 字段作用分工

| 字段 | 用于算子面板（OperatorPalette） | 用于 LLM 生成工作流 | 用于验证（ValidationChain） |
|------|-------------------------------|--------------------|-----------------------------|
| `name` / `display_name` | 展示算子名称 | 算子引用标识 | 算子存在性检查 |
| `category` | 分类展示 | LLM 筛选参考 | - |
| `brief_description` | 悬浮提示 | LLM 筛选参考 | - |
| `parameters` | 参数配置面板渲染 | 参数名/类型参考 | 必需参数、参数名称验证 |
| `outputs` | - | 输出引用参考 | 参数引用格式验证 |
| `workflow_example` | - | **最重要**：LLM 参考此格式生成 tasks | - |
