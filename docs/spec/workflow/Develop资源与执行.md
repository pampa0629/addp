# ADDP 工作流 Develop 资源适配与执行规范

[返回工作流计算引擎接口规范](../addp工作流计算引擎接口规范.md)

## 算子契约分层

工作流算子契约必须拆分为以下三层，三层职责不得混入同一个 `parameters[]`：

1. **Public Operator Spec**：面向用户、前端、AI 和 Develop 任务定义，声明公开参数、资源选择方式和校验规则。读取已有资源使用 `locator`，创建目标使用 `target_parent_locator + target_name`。
2. **Develop Adapter Spec**：由 Develop Backend 按当前 workflow engine type 和 operator id 显式选择，声明公开资源参数如何转换为运行时参数，并负责权限校验、System Engine Instance 查询和连接信息派生。
3. **Runtime Operator Spec**：由 Workflow Runtime 暴露和消费，只声明运行时真实需要的参数、输入输出端口和执行行为。

`ResourceLocator` 只属于 Public Operator Spec。GeoPython Workflow、Spark Workflow、SuperMap Workflow 等运行时不得解析 `addp://` locator。

`connection_info`、`engine_id` 以及由资源身份派生的 `schema/table/path` 属于 Develop Adapter Spec 到 Runtime Operator Spec 的内部参数。用户、前端和 AI 不得直接提交这些内部参数；只有在对应 Public Operator Spec 明确将同名字段定义为公开业务参数时才可例外。

需要同时读取源存储并写入目标存储的持久化转换算子，Runtime Operator Spec 使用 `addp.workflow.access-plan/v1` 工作流访问计划，不使用单个共享 `engine_id/connection_info` 表达两端资源。源与目标必须分别由调用方解析为执行期访问参数；运行时仍不得解析 ADDP `ResourceLocator`。

Develop Backend 不得仅因任意算子的参数中出现 `locator`、`target_parent_locator` 等固定名称就触发派生。每个需要资源适配的算子都必须存在显式 Develop Adapter Spec；未声明的算子携带公开资源参数时必须拒绝执行，不得回退到隐式派生路径。

资源选择器按存储实例能力、计算引擎接入能力和算子操作要求取交集。直接连接路径的读写范围唯一来自当前 Runtime Engine 的 `compute.workflow.storage_access`；Develop Adapter 只声明资源输入输出及节点形态，不维护引擎类型名单。Develop 按输入读取范围或输出写入范围生成 `ui_config.engine_types`，该字段只是引擎声明的 UI 投影，不是算子定义中的事实源。执行前必须重新读取当前 Runtime 实例声明并校验源、目标；已有任务和直接提交的资源不能绕过。目标还必须通过真实存储引擎原生目录模型校验，不得通过伪造 locator 类型切换访问方式。单个直接连接适配只能绑定一处资源；同时提交源和目标必须拒绝，不能让后一个资源覆盖共享连接。分别读写两端资源的操作必须使用已声明的 access-plan 契约。

Spark `save` 当前只支持 PostgreSQL、MySQL、Doris、MinIO。NFS 尚未消费真实服务器与导出路径，不作为候选；MinIO 继续遵守普通 Parquet `create` 成果契约。受控 access-plan 算子按对应 Provider 能力校验，不消费直接连接器范围；选择器类型范围由 Common 访问计划解析器的实际访问方法映射派生。算子可约束所需访问方法，例如 SuperMap OSGB Scene 输入必须是 `mounted_path`，不能在适配器中重复写 NFS 类型名单。

Develop 的算子发现 API 必须为前端提供独立的 `public_parameters`。前端工作流编辑器、参数面板、输入连线和任务保存只允许消费 `public_parameters`，不得读取 Workflow Runtime 返回的 `parameters`。`parameters` 保留为 Runtime Operator Spec；在 Runtime 元数据完成净化前，即使其中仍存在历史 UI 或公开资源字段，也不得再作为前端契约使用。

Public Operator Spec 由 Develop Adapter Spec registry 统一维护。Workflow Runtime 的 `parameters[]` 禁止出现 `ui_type=resource_tree_picker`，禁止出现 `locator`、`target_parent_locator`、`target_name` 等 ADDP 公开资源身份参数；runtime 必须直接声明自身真实消费的 `connection_info/schema/table/path` 等执行参数。

Develop 聚合算子时必须校验 Runtime Operator Spec：发现 UI 参数或公开资源身份参数时拒绝该运行时算子目录；存在 Develop Adapter Spec 时，runtime `parameters[]` 必须完整声明 adapter spec 所需的运行时参数。不得在参数缺失时继续展示算子并等待执行阶段失败。


## 执行语言接线

Develop 在一次性执行和已保存任务的执行快照中冻结 `display_language`；由 Orchestrator 触发的子执行从已核验父记录继承，不从子请求重新取语言。审批式 workflow.run 使用最初提交语言。Worker 从快照恢复后经 Common Workflow Provider 发送规范 `Accept-Language`；任务定义、算子参数和授权摘要不承载该值。完整契约见[执行显示语言](../tasks/执行记录与租约.md#执行显示语言)。

## 集成架构

Develop 模块通过 Common Engine 的 `WorkflowRuntimeProvider` 统一调用各工作流引擎：

```
Develop Backend (Go)
├── internal/api/workflow_handler.go         # API 层：接收前端请求
├── internal/service/workflow_engine_service.go  # 服务层：通过 common/engine provider 调用运行时
└── internal/models/execution_config.go      # 配置模型：执行参数
```

## 调用流程

```go
// 1. 前端先向 Develop Backend 获取可用工作流引擎实例
GET /api/v1/develop/workflow-engines

// 2. 用户选择具体工作流引擎实例后，前端按实例 ID 获取该实例的算子目录
GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators

// 3. Develop Backend 查询该引擎实例，确认它已启用且具备 compute.workflow 能力
engine := systemClient.GetEngineByID(workflowEngineID)
assert engine.IsActive && SupportsComputeEntrypoint(engine, "workflow")

// 4. Common Engine 校验实例 capabilities 后创建通用 HTTP WorkflowRuntimeProvider
provider := commonEngine.GetWorkflowRuntimeProvider(engine)

// 5. Provider 使用该实例 Runtime Descriptor 中的 endpoint，按 addp.workflow/v1 调用 GET /api/operators
operators := provider.ListOperators(ctx, engine.ConnectionInfo)

// 6. 执行工作流时，Develop 仍使用工作流引擎实例 ID 作为 execution_config.engine_id
resp := provider.ExecuteWorkflow(ctx, engine.ConnectionInfo, workflowRequest)
```

Develop 对前端暴露的算子发现主路径必须是 `GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators`。`workflow_engine_id` 是 System 中已注册的具体工作流运行时实例 ID；API 路径不得包含 `geopython_workflow`、`spark_workflow`、`math_workflow` 等具体 `engine_type`，也不得使用 `module` 表达算子来源或分组。这样用户动态注册的新扩展工作流引擎只要具备 `compute.workflow` 能力，就能通过同一条路径被 Develop 发现和消费。

Develop 不提供按 `engine_type`、`module` 或全局汇总查询算子的公开 API；所有上层调用方必须先选择具体工作流引擎实例，再通过实例 ID 路径获取算子。这样可以避免同一 `engine_type` 多实例、用户扩展引擎或同名算子场景下发生混淆。

Develop 的算子发现 API 是工作流编排视角，只返回 `execution_modes` 显式包含 `workflow` 的算子。只支持 `direct` 的算子不得出现在工作流画布或 Copilot 工作流生成链路中；业务模块若需要调用 direct 算子，必须通过受控的单算子调用路径，并在调用前校验目标算子显式支持 `direct`。

Copilot 等上层智能生成链路也必须沿用同一实例 ID 契约：生成工作流时只接收 `workflow_engine_id`，不得要求前端额外提交 `engine_type` 作为生成上下文或算子发现条件。工作流引擎类型只能由 Develop/System 后端按实例 ID 查询得到，不能成为 API 输入侧的第二事实源。

### Copilot 工作流入口身份

`POST /api/v1/copilot/workflow/generate` 使用同一条生成路径，按调用凭据区分校验。身份事实来自 System AuthContext，普通用户与委托的通用规则见 [OAuth AuthContext 映射](<../addp OAuth授权规范.md#八authcontext-映射>)及[受委托访问令牌](<../addp OAuth授权规范.md#九受委托访问令牌>)。

| 调用身份 | 接收与授权要求 | 资源确认与生成边界 |
| --- | --- | --- |
| 普通用户直调 | 接受第一方 Web User Access Token 或 OAuth User Access Token；要求 User Principal、当前 Tenant Context 和 `copilot.workflow.execute`。OAuth User Token 仍须按已批准的 Scope 收窄访问。 | Develop 页面使用当前用户令牌。缺少 `resources[]` 时先发现候选并等待用户确认，确认后生成候选 DAG。 |
| Agent Tool 委托 | Agent 调用必须使用 System 签发的 Delegated Access Token；要求 User Principal、当前 Tenant Context、唯一 audience `copilot`、精确 Scope 集合 `["workflow.draft.generate"]`，以及同一 `copilot.workflow.execute` Permission。委托来源、AgentRun、ToolCall 和有效性遵循 OAuth 规范。 | Agent 先通过 Owner Tool 确认资源，再提交 `resources[]`。空资源不在 Copilot 重新发现；现有 WorkflowService 返回 `status=need_clarification`、`clarification_reason=resource_facts_required`，不视为生成成功。 |
| 服务身份 | Service Access Token 不得作为本生成端点的入站身份，不能代替普通用户或 Agent 委托。 | Copilot 自身向 Develop、Inference 发起内部调用的服务身份按下文保留；Standard、Graph 等其他 Copilot 端点继续遵循各自的服务身份契约。 |

普通用户直调不要求页面先兑换 Agent 委托令牌。Agent Tool 不得把原始 User Access Token 直接转发给 Copilot；入站委托令牌也不得转发给 Develop。`resources[]` 是受限生成上下文，不是资源授权或真实性凭据；候选生成不授予保存、发布或执行权限，后续操作继续由 Develop 独立校验。

Copilot 在同一 Tenant Context 中读取工作流引擎列表和 Public Operator Spec 时，必须使用 `addp-copilot` Confidential OAuth Client 获取的 Tenant Service Access Token，并且只发送 Bearer。`tenant.copilot_runtime` 只授予该链路所需的 `develop.task.read` 和推理执行权限，不授予工作流保存、修改或执行权限；调用不得发送 `X-Internal-API-Key` 或 `X-Tenant-ID`。

### 当前守卫缺口与验收边界

上述为统一契约；以下差异尚未修复，不能把规范要求写成当前已具备的保护：

- 实际路由使用 [require_tool_user](../../../copilot/backend/dependencies/auth.py)，只显式检查 Tenant ID、委托条件和 Permission，未显式限定 User Principal，也未落实普通 OAuth Token 的 Scope 收窄校验。不能宣称当前路由已保证拒绝服务身份。
- [allows_delegated_tool](../../../common-python/addp_common/auth.py) 对非委托令牌直接返回允许，对委托 audience 和 Scope 使用包含判断；owner 的 Scope 精确集合检查仍有缺口。这里描述 owner 校验不足，不表示 System 已签发超范围委托令牌。Swagger 的 `delegated_tool` 标记只声明契约，不会自动安装运行时守卫。
- [工作流认证测试](../../../copilot/backend/tests/test_workflow_auth.py)中的服务主体拒绝用例调用 `require_user`，未覆盖实际路由的 `require_tool_user`。已有直接调用 Handler 的资源发现用例和 OpenAPI 声明检查，也不能代替已挂载路由的认证、授权回归。

后续回归须覆盖同一真实路由上的第一方与 OAuth 用户、合法与越界委托、缺少 Permission／Tenant、服务主体拒绝，以及普通用户资源确认和 Agent 不重新发现的差异；另核对 Copilot 内部调用没有转发入站用户或委托令牌。若调整共享守卫，须复核 Query、Notebook、Transfer 等消费者，并按[测试与验收规范](../addp测试与验收规范.md)选择 `make test-module MODULE=copilot`、共享依赖对应门禁及 `make test-changed` 的扩散范围。

本节不提供上述缺口已修复或已通过验收的证据。静态源码核对、Handler 单测、真实路由回归与 System → Gateway → Copilot／Develop 的运行验收分别记录；真实模型调用按[推理验证要求](<../addp AI推理接口规范.md#九验证要求>)独立验证。未运行、失败和未覆盖项不计为通过。

## 工作流算子资源参数

用户、AI 和前端工作流画布配置数据源/目标时，资源身份必须使用标准 `ResourceLocator`：

- 读取已有资源：使用 `locator`，例如表/集合 `addp://engine/12/path/public/roads?type=table&item_id=99`，NFS 文件 `addp://engine/3/path/data/roads.csv?type=file&item_id=45`，对象存储对象 `addp://engine/4/path/addp/lake/roads.parquet?type=object&item_id=46`。
- 创建新目标：使用 `target_parent_locator + target_name`。父 locator 必须指向真实父节点，例如 schema/database、NFS root/directory、对象存储 bucket/prefix；不得提前构造尚不存在的虚拟目标 locator。

Develop Backend 在调用 `WorkflowRuntimeProvider.ExecuteWorkflow()` 前负责把上述公开参数派生为运行时参数：

- table/collection：派生 `connection_info + schema + table`。
- NFS/file：派生 `connection_info + path`，其中 `path` 为挂载根内相对路径。
- MinIO/S3 object：派生 `connection_info + path`，其中 Spark Workflow 的运行时 `path` 使用 `s3a://bucket/key`。

算子 `source_type` / `target_type` 表达访问形态，统一使用 `table`、`file`、`geojson` 等值；不得使用 `nfs`、`minio`、`s3` 等存储引擎类型作为算子数据源类型。具体存储引擎类型只来自 locator 对应的 System engine 及派生后的 `connection_info.engine_type`。

Develop Adapter Spec registry 为 Public Operator Spec 提供资源选择器 `ui_config`。适配器只按操作所需的资源形态声明 `engine_families`、节点类型、格式与其他操作约束，不得内嵌具体 `engine_types` 白名单。直接连接路径的 `ui_config.engine_types` 必须由当前计算引擎实例的 `compute.workflow.storage_access` 生成，并与存储实例能力、`engine_families` 取交集；未声明接入范围时生成空列表，不允许选中任何直接连接资源。不同实例可以声明不同范围，Develop 不按 `spark_workflow` 等固定类型生成名单。Workflow Runtime 的 `parameters[]` 不得包含 `ui_config` 或任何资源树 UI 参数。

### 持久化转换访问计划

`vector_to_pmtiles` 统一消费下述 `addp.workflow.access-plan/v1`，删除 `source.root_uri/source.gdal_env/target.archive_uri/target.gdal_env` 参数路径。Manager 的快显、业务瓦片集及数据库临时 FlatGeobuf 均使用同一契约；数据库源的准备和临时材料生命周期仍归 Manager。进度回调是执行期计划的附加字段，不进入用户定义。

Develop 将该算子显示为“矢量切片”，位于“矢量处理 / 成果生成”。首版输入为已扫描的 Shapefile 或 GeoJSON 文件／对象，公开参数为 `locator`、`target_parent_locator`、`target_name`、`write_mode`、`min_zoom`、`max_zoom`、`layer_name` 和 `attributes`。`attributes` 是显式属性字段名数组，缺省为空；输出始终保留几何，Manager 的可选主键仍按原配置保留，不隐式输出全部属性。字段、源格式、范围和 CRS 从 Meta 事实校验／派生，缺失或失配时要求扫描，不允许用户提交内部 `tile`、访问计划、GDAL URI 或凭据。Develop 不接入数据库空间表或多图层容器。

运行时参数仍为 `access_plan + tile + options`，`tile` 只由调用方派生；运行时复核字段和 CRS。按元数据 attributes 规范，Meta 中的范围属于源数据的原生 CRS，因此 Develop 使用同一源 SRID 派生 `tile.source_srs` 和 `tile.extent_srid`，不要求额外的范围 CRS 元数据字段。输出必须是 `format=pmtiles + layout=single` 的 PMTiles v3 单文件，复用标准 `create/replace` 发布。Develop 生成命名稳定输出、`produced_targets`、真实来源血缘和 Meta 扫描；Manager 私有缓存与业务瓦片集各自保留现有归属。两端访问参数不进入长期结果。

文件、对象和目录型持久化转换统一使用以下内部契约：

矢量切片的源 GDAL 检查、CRS 复核和范围转换在独立子进程内执行，避免原生库上下文跨 HTTP worker 的 fork／线程边界；源凭据只传给源读取子进程，发布继续由标准目标发布器负责。原生回归须从正式 Gunicorn 入口验证 CRS 调用后 worker 与共享执行状态仍保持有效。

```json
{
  "schema_version": "addp.workflow.access-plan/v1",
  "source": {
    "kind": "file",
    "format": "las",
    "entrypoint": "sample.las",
    "access": {
      "method": "mounted_path",
      "path": "/mnt/business/sample.las"
    }
  },
  "target": {
    "kind": "file",
    "format": "copc",
    "name": "sample.copc.laz",
    "write_mode": "create",
    "access": {
      "method": "object_store",
      "endpoint": "minio:9000",
      "bucket": "research",
      "object": "pointcloud/sample.copc.laz",
      "use_ssl": false
    }
  }
}
```

稳定规则：

1. `schema_version` 固定为 `addp.workflow.access-plan/v1`。
2. `source.kind` / `target.kind` 只表达 `file` 或 `directory`；`format` 使用平台规范格式标识。
3. `access.method` 第一版只允许 `mounted_path` / `object_store`。对象存储的下载、staging、presigned URL 或本地工作目录属于运行时实现，不扩展为 Public Operator 参数。
4. `target.write_mode` 只允许 `create` / `replace`，默认 `create`；不得在 `create` 失败后隐式回退为覆盖。共享发布器确认目标已存在时抛出类型化冲突，Python DAG 包装与异步 execution 快照保留稳定 `error_code=TARGET_ALREADY_EXISTS`；Develop 将该终态映射为 `stage=result`、`reason=target_exists` 的受控诊断。不得解析错误文案判断冲突，通用提示不得携带目标路径或连接信息；该诊断只表示本次发布拒绝覆盖，不保证整个 DAG 尚无其他产出。
5. 访问计划只在执行期存在，可以携带临时连接信息；用户任务定义只保存 `locator`、`target_parent_locator + target_name` 和公开业务参数。
6. 调用方必须同时生成不含密钥的 audit plan，用于领域结果或统一执行记录；不得把运行时访问计划原文长期保存。
7. Manager infra 目标和 Develop 业务目标分别在调用方领域边界内解析，再进入同一个访问计划构造器。Develop 的源和目标存储必须是当前租户的业务 Engine Instance，不得选择 `tenant_id=nil` 的平台 infra 存储；Manager 可按私有 artifact 生命周期显式构造 infra 目标。访问计划不表达 owner module、artifact/data item 归属或 Meta 扫描策略。
8. Runtime 完成写入后只返回转换结果事实。Manager 负责登记私有 artifact；Develop 负责记录 `produced_targets`，在统一 execution 成功结果中写入 `lineage_facts`，随后立即通知 Meta 采集并提交 Meta scan。工作流血缘根据本次冻结的实际参数和 `$ref` 数据流按产出节点生成，输入、输出端口分别绑定读取节点、产出节点；独立分支不能共享来源，单纯 `depends_on` 不生成来源边。Meta scan 是异步的，目标 Item 尚未落库时由周期 collector 负责重试；用户 Item refresh 不承担血缘触发职责。
9. 每个 Runtime Operator Spec 必须显式声明 `effects`；允许值与效果含义统一见[算子元数据规范](算子元数据与实现.md#算子描述结构)。
10. Develop 在执行前按整个 DAG 汇总效果，并结合源/目标 locator 和当前 User AuthContext 创建 Execution Authorization。客户端提交的效果、Runtime 返回值或 Engine 创建人均不能替代服务端汇总结果。
11. Workflow Access Plan 只承载本次运行所需的临时访问参数；其允许效果不得超过 Execution Authorization。Runtime 收到计划后必须按算子声明再次拒绝越权效果，不得把连接信息、访问计划或 Token写入运行时状态、统一 execution 或用户任务定义。

Python 实现的 Workflow Runtime 应共享 `common-python` 中的协议执行核心，包括 workflow definition 校验、DAG 拓扑排序、引用解析、异步 execution 状态和标准错误；GeoDataFrame、Spark DataFrame、PDAL、三维转换器等领域执行仍保留在各自运行时。该公共核心是库，不是新的 System Engine Instance 或独立服务。

资源选择器必须通过 `ui_config.resource_binding` 显式声明参数绑定，前端不得按算子 ID 或固定参数名猜测。读取已有资源使用 `mode=existing + locator_param`；创建目标使用 `mode=target + parent_locator_param + name_param`。需要同步公开访问形态时声明 `type_param + type_values`，需要选择后填充默认公开参数时声明 `default_params`。

资源选择后需要绑定标准元数据事实时，也必须由 `resource_binding` 显式声明目标参数。例如 `geometry_column_param` 表示从所选 item 的 `capabilities.spatial.geometry_columns[]` 读取可用几何列：单列自动选择；多列默认第一列并允许用户改选；无几何列时不得显示手工文本输入框。前端不得猜测 `geom`、`geometry` 等固定字段名。

## 工作流任务执行输入与输出

保存成功的 Develop 工作流必须包含完整、可直接执行的公开参数；执行输入只允许对本次 execution 覆盖保存值，不能用于补齐不完整任务。工作流任务的执行输入契约由 Develop 根据当前 `workflow_definition`、目标 Workflow Runtime 的 Public Operator Spec 和 Develop Adapter Spec 动态生成，不在任务内容中复制保存第二份 Schema 或默认值。

执行输入自动包含每个节点中未被内部数据连线占用、可序列化且可完整校验的 Public Operator 参数：

- 标量、枚举和普通对象参数按 `task_id -> public_parameter` 分组暴露。
- 已由 `{ "$ref": "<task_id>" }` 提供的输入参数不允许外部覆盖。
- `operator`、`id`、`depends_on`、工作流运行引擎绑定等结构事实不属于执行输入。
- `connection_info`、`engine_id`、`schema/table/path`、Workflow Access Plan 和 Adapter 派生参数不允许进入执行输入。
- 没有提交覆盖值时始终使用 `workflow_definition` 中保存的值；覆盖值只写入本次 execution 快照，不回写任务定义。

资源选择参数必须作为一个逻辑参数组进入执行契约。读取资源至少包含 `locator`，可按 `resource_binding` 同时包含 `geometry_column` 等用户需要确认的关联事实；目标资源使用 `parent_locator + name`，并可包含公开 `write_mode`。执行时更换资源后必须重新读取资源事实、刷新关联字段并重新校验，不得沿用旧资源的几何列、类型或格式事实。

跨任务输出只允许声明可持久定位、可序列化且能在任务边界外重新解析的稳定结果，例如保存算子生成的 ResourceLocator。DataFrame、GeoDataFrame、Spark DataFrame、SuperMap 内存句柄和其他 Runtime 私有对象只能通过当前工作流内部连线传递，不得进入 TaskProvider 输出契约。每个可编排输出必须使用稳定输出名而不是数组下标；Develop execution 结果应按产生节点返回命名输出，并继续保留 `produced_targets` 作为产物摘要。资源输出的 `resource` 包含 `locator`、`type` 和可选 `write_mode`，输出 Schema 必须声明该已有写入事实以供血缘消费；模式为 `create/replace/append/upsert`，栅格访问计划仅允许 `create/replace`。

GeoPython 通用文件保存的公开 `mode=fail` 表示目标已存在时失败，成功保存后的文件稳定输出和血缘事实必须记录标准 `write_mode=create`。该映射以已确认的 `file` 目标为边界，不改变运行时原有拒绝覆盖行为，不将未知写入模式或表目标推断为 `create`。多个保存节点分别保留各自写入事实；Meta 继续对缺少已知模式的 derive 只保存观察、不投影当前关系。

Orchestrator 为每个执行输入只允许三种互斥来源：使用任务保存值、设置 Step 固定覆盖值、引用显式 `depends_on` 上游 Step 的已声明稳定输出。不得继续让用户手写未校验的结果路径或把整个上游原始响应作为下游输入。

同一算子能够读取多类资源时，应使用一个资源选择参数并由 locator 对应的资源事实决定运行时访问方式，不得要求用户预先选择 `source_type`。文件格式应优先由所选资源的格式事实或路径扩展名确定；支持格式列表属于资源选择器过滤条件，不应作为必选算子参数。内存对象输入应通过算子输入端口传递，不得在资源加载算子中另设 `geojson` 等旁路参数。

Spark 文件加载的 `format` 是 Develop 资源 Adapter 派生的运行时参数。文件和对象 locator 使用共享格式识别器根据原始文件名确定格式，任务不保存独立的格式参数；Spark 必须显式消费该结果，不得在缺失时默认按 Parquet 读取。无法识别或运行时不支持的格式必须拒绝读取。

资源 Adapter 只派生其声明的运行时参数。GeoPython 通用 `load` 从派生后的文件路径扩展名识别格式，其运行时签名与 Adapter 均不声明 `format`，Develop 不得向该算子注入 Spark 的 `format` 参数；文件和对象来源遵守同一规则。

Spark 的 `load` 同时支持 Elasticsearch 的具体 `index` locator。目录仍为 `service → index`，索引的结构化消费仍使用既有 `table` 数据类型；`source_type=index` 只是运行时访问分支，不是新增数据类型。Develop 按当前用户授权，在执行期从 locator 派生单段索引名 `index` 和 System 连接事实；任务不保存这些派生字段，Spark 集群仍通过独立的 `spark_cluster_id` 选择。GeoPython 的加载和所有保存算子不因此获得 ES 能力。

ES 首版 Spark 加载固定使用官方 `elasticsearch-spark-30_2.12:9.5.4` 完整 JAR，校验 SHA256 并由 Spark 分发给 Executor，不通过该制品的 Maven POM 引入另一版 Spark/Scala。仅支持 HTTP Basic 认证的普通具体索引批量读取；HTTPS 明确拒绝，不自动降级，不开放别名、data stream、隐藏索引、通配符、任意连接器选项、DSL、流式或写回。关闭节点发现，只访问登记端点；Driver 和 Executor 必须同时可达该端点。本地回环地址按既有 `SPARK_WORKFLOW_SHARED_HOST` 部署事实派生，不能修改 System 中的原始连接。

字段结构由实时 Mapping 决定。公开可选参数 `array_fields` 只声明 Mapping 无法区分的单值/多值字段，例如 `["tags"]`；字段必须存在于 Mapping 的 `_source` 结构中，不能是 multi-field 或通配表达式。`nested` 已固定为对象数组，不重复声明；不得抽样猜测数组或把不一致的结构强制转成字符串。Spark 内部保留 `long/bigint`，JSON 结果摘要中超出 JavaScript 安全整数范围的值使用十进制字符串，Schema 仍保留原类型。连接器的分片 scroll 读取不承诺全工作流或多次 Spark action 共享同一个 PIT 快照。

Business ES reader 仅增加 `cluster:monitor/main` 和 `cluster:monitor/health` 两项集群读取动作，索引权限保持 `addp_*` 的 `read + view_index_metadata`；不增加节点发现、集群配置或写入权限。HTTP 首版属于开发实验接入；已有 Go ES 插件的 HTTPS 与 PIT 读取契约保持独立。该限制来自官方连接器 9.5.4 的主机名校验实测差异，不能以一次 Driver 证书预检代替 Executor 每次连接的服务端身份校验。

同一算子能够写入表格或文件目标时，也应使用一个目标父资源选择参数，由父 locator 类型决定 Adapter 派生 `schema/table` 或 `path`，不得要求用户预先选择 `target_type`。文件输出格式由目标名称扩展名确定，不应再公开独立 `format` 参数。

`connection_info`、`schema`、`table`、`index`、`path` 和用于派生连接的存储引擎 `engine_id` 都是 Develop 到运行时之间的内部参数，不应作为算子公开填写项；Develop 不接受工作流任务参数直接提交存储引擎 `engine_id` 作为旧式资源身份。Spark Workflow 顶层 `engine_id` 只绑定实际 `spark` 通用引擎资源，与数据源 locator 中的存储引擎 ID 不是同一概念。

Copilot 在生成工作流前做数据源理解时也必须遵守同一资源契约：低置信度或未验证的数据源只能返回 `DataSourceCandidate[]` 给调用方澄清，候选项中的位置字段使用 `namespace`、`table`、`bucket`、`path` 作为解释性事实，并提供标准 `locator` 或 `target_parent_locator`；不得把元数据搜索结果中的 `schema` 字段透传为 Copilot 对外模型字段，也不得把存储引擎 `engine_id` 写入工作流任务 params。

---

## Spark 对象存储 Parquet 成果

Spark 保存到 MinIO 时，公开定义保存 `target_parent_locator + target_name`，执行期由 Develop 按 execution UUID 和算子节点身份派生独占子目录。每次执行创建一份新的普通 Parquet 成果，不覆盖或追加既有目录；公开保存模式为 `create`，拒绝 `overwrite`、`append` 和其他输出格式。多个保存节点不能共用同一个成果目录。

成果复用已有 `table + format=parquet + layout=whole`。ResourceLocator 定位完整 scope 的 data item；Meta 精确扫描另按同一范围的 prefix 定位，prefix 是扫描范围，不是第二份成果身份。Develop 在请求自动扫描前，必须同时持久化现有 `metadata.outputs` 和 `metadata.result.produced_targets`；Meta 只有在同一输出节点的产物摘要声明 `layout=whole`，且 Engine、对象 Locator 和完整路径均与稳定输出一致时，才允许将该对象身份转换为同一路径的 prefix 扫描范围。普通单对象不得借此扫描其同名前缀，父目录、兄弟目录及多目标范围均不接受。Spark 下游文件加载从 Meta 的正式 item facts 派生 format/layout，不从目录名称后缀猜测格式。

MinIO 连接唯一来自本次已授权的存储 Engine；Spark 集群连接不得携带业务存储凭据。Spark Driver 与 Executor 必须访问同一存储端点。JDBC、ES 与 S3A 的回环主机统一按 `SPARK_WORKFLOW_SHARED_HOST` 派生执行地址；S3A 保留原协议与显式端口，远程主机地址不改写，System 的连接事实不变。缺少或非法的共享主机配置须在存储访问前拒绝。读写配置限制在本次操作的 Hadoop Configuration，并禁用按 bucket 缓存的 S3A FileSystem，防止同名 bucket 的不同端点或凭据互相污染。保存及成果核验要求目标引擎 read + write，不自动新增 Resource Grant。

使用与 Spark/Hadoop 版本匹配的 S3A Magic Committer。成功须核验提交标记、全部 Parquet 分片的 schema 和真实行数，再返回稳定 outputs 并触发异步 Meta 扫描。失败清理只读取本次独占目录中的 Magic Committer `.pending` / `.pendingset` 记录，先用 Hadoop 校验全部记录并核对 bucket、目标 key 的目录边界及 upload ID，再精确取消这些上传并删除本次目录。不使用 MinIO 的目录前缀 multipart 列表作为清理证据。没有 pending 记录时，只有有效的 Magic 提交成功标记允许删除已提交成果；记录缺失且没有成功标记、记录损坏或取消失败时，报告“清理未核验”并保留目录和记录。清理只保证已记录上传，不承诺发现崩溃前未落盘的上传；不删除父目录、兄弟成果或 bucket 范围的上传。进程被强制终止时可能残留目录，不承诺自动恢复。普通 Parquet 多对象写入不具备数据集级原子可见性；独立扫描或外部读取仍可能看到提交中的对象，不能宣称 PostgreSQL 式事务发布。自动 Meta 扫描失败不改写已完成的存储提交事实。
