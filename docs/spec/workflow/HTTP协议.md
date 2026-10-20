# ADDP 工作流运行时 HTTP 协议

[返回工作流计算引擎接口规范](../addp工作流计算引擎接口规范.md)


## 标准接口清单

所有工作流引擎**必须**实现以下 5 个标准 API 接口：

| 接口 | 方法 | 路径 | 说明 |
|------|------|------|------|
| **健康检查** | GET | `/health` | 检查引擎状态 |
| **算子列表** | GET | `/api/operators` | 获取算子元数据 |
| **工作流执行** | POST | `/api/workflow` | 执行完整工作流（DAG） |
| **单算子调用** | POST | `/api/operators/<name>/invoke` | 受控调用单个算子，不进入 ADDP 任务体系 |
| **执行状态查询** | GET | `/api/executions/<execution_id>` | 查询执行状态 |

## 接口详细规范

### 健康检查

**请求**：`GET /health`。

Math 的响应示例与字段定义统一见 [OpenAPI](../../../engines/docs/workflow-engine-api-v1.yaml) 的 `GET /health` / `HealthResponse`；示例返回 `status`、`service`、`version`、`uptime` 和 `operators_count`。`uptime` 单位为秒，运行时长与算子数量以实际实例为准。

`/health` 表达 **Runtime 整体就绪**：当前算子目录所承诺能力的本地必需条件（工具、SDK、许可等）全部满足时，只返回 HTTP `200` 与 `status="healthy"`；任一必需条件不满足或健康检查失败时，只返回 HTTP `503` 与 `status="unhealthy"`。两种结果都必须返回 `service`、`version`，不得用 HTTP 200 表达 unhealthy，也不保留 `degraded`、`conversion_ready` 等并行判定。

`dependencies` 可省略；提供时为按依赖名称索引的对象，每项必填布尔值 `available` 和 `required`，可附 `version` 与 `details` 字符串。只返回本地绑定事实，不返回凭据、连接参数、堆栈或下游响应正文。依赖缺失的诊断应指出所需工具或许可，避免暴露宿主机路径。可选依赖不可用不影响 healthy；支持已声明算子或已声明接入方式所必需的依赖不能标为可选。Model3D 任一已声明转换工具缺失时，整个 Runtime 返回 unhealthy，不按部分可执行算子接单。

健康检查不连接某次任务的源端或目标端，不校验任务数据和权限，不依赖 System 注册成功；Spark Workflow 不因尚未选择任务的 Spark 集群而不健康。Python Runtime 复用 Common Python 的健康响应构造，Common Go Provider 统一校验 HTTP 状态、必填字段、结构化依赖及状态一致性；空体、非法 JSON、未知状态、旧依赖字符串或 `healthy` 同时存在不可用必需依赖均为探测失败。HTTP `/health` 不替代业务 Backend 的 `/health/live`、`/health/ready`，也不扩展 Engine Instance 的生命周期或 `connection_status` 枚举。

### 获取算子列表

**请求**：`GET /api/operators`。可选 `category` 按该运行时元数据中的实际分类值过滤。

响应结构见 [Math 算子列表示例](../../../engines/docs/examples/operator-list-response.json)。示例仅保留 `add` 展示字段，`count` 与示例数组长度一致；实际算子目录与数量以运行时返回为准。同一响应中所有算子的 `engine_type` 必须与所注册的 Runtime 类型一致。

### 执行工作流

**请求**：`POST /api/workflow`，`Content-Type: application/json`。

完整请求见 [Math Workflow 示例](../../../engines/docs/examples/workflow-execute-request.json)：先计算 `10 + 20`，再通过 `$ref` 将结果传入乘法节点，最终结果为 `60`。示例的 `runtime` 表达后端注入的执行上下文，字段来源与授权要求见下文。

`engine_id` 仅用于 `spark_workflow` 的执行期绑定，必须位于请求顶层，指向已注册且启用的 `engine_type=spark` 通用引擎资源。Develop 从 `execution_config.engine_specific.spark_cluster_id` 派生该值；其他 Workflow Runtime 的请求不得携带此字段。

`input_data` 是可选的外部输入对象；提供时必须为 JSON object，不能为 `null`。共享 Go 客户端在没有外部输入时发送 `{}`，不改变 Runtime 的对象校验契约。

`runtime.tenant_id` 是 Workflow Runtime 的必填租户上下文。它必须由拥有工作流执行入口的 ADDP 后端模块从已验证的 AuthContext 派生并注入；浏览器、前端模块和外部客户端不得直接提交或覆盖该值。Runtime 使用该租户上下文获取 Tenant Service Access Token、访问租户资源，并隔离租户级运行时状态。

`runtime.execution_authorization` 是 Workflow Runtime 的必填执行期授权摘要。`id` 必须为正整数；`effects` 必须是非空、无重复的 `read | write | ddl | external_effect` 集合，并覆盖本次 DAG 全部算子声明的 `effects`。Runtime 必须在执行前重新聚合 DAG effects 并拒绝缺失授权、未知效果或授权不足的请求。该摘要不是 Access Token；Runtime 不接收 User Token、Service Token 或明文 Engine 连接作为授权事实。

**响应**：
```json
{
  "status": "success",
  "execution_id": "uuid-1234-5678",
  "final_result": 60,
  "all_results": {
    "task1": 30,
    "task2": 60
  },
  "execution_time_ms": 12.34
}
```

`final_result` 和 `all_results` 必须可 JSON 序列化。对于 Spark DataFrame 等大规模运行时对象，工作流引擎不得把完整数据集塞入响应；应返回轻量结果摘要，例如类型、schema、少量预览行和预览上限。需要持久化或跨模块消费完整数据时，必须通过 `save` 等算子写入目标资源，并在结果中返回目标资源引用或保存摘要。

Spark 摘要中的 Decimal 使用精确的定点十进制字符串，保留原小数位，不经浮点数或科学计数法转换；例如 Decimal(38,18) 的最小正数和零分别输出 `"0.000000000000000001"` 与 `"0.000000000000000000"`。此规则递归应用于最终结果和各节点摘要，并由执行状态查询复用同一摘要；null、空串和字符串尾随空格保持原样。

Spark `save` 保存 PostgreSQL 结果时，普通列与 Sedona Geometry 列统一走同 schema 唯一暂存表。Worker 完成 JDBC 写入后，Driver 在一个 PostgreSQL 事务中转换空间列、核对暂存行数并发布结果。目标不存在时将暂存表改名；目标已存在时锁表，校验字段和类型，`overwrite` 使用 DELETE + INSERT，`append` 只 INSERT。不得删除或重建已有目标表，表结构、约束、索引、权限、注释及依赖保持不变；约束失败或提交前异常须确认回滚后才能清理本次暂存表；回滚未确认时保留暂存证据和原始错误。已有目标的暂存表在提交确认后另行清理，不在发布事务内删除；新目标继续在事务内由暂存表改名。COMMIT 发出但未获得成功确认时，明确报告发布结果不确定，保留暂存或已改名目标的现有证据，不宣称回滚成功，不自动清理或重试发布。提交成功后的资源关闭或暂存清理失败不得改写已提交事实；清理未核验须留有诊断。Spark JDBC 的 Worker 重试不承诺暂存记录去重；发布事务之外的外部触发器副作用和提交响应丢失也不提供恰好一次保证。

已有目标必须与结果拥有相同字段集合和 PostgreSQL 基础类型；目标类型修饰符须相同或不设限。PostGIS geometry 的子类型与 SRID 由目标约束验证；自动生成列、identity 列和非普通表目标拒绝写入。DELETE 影响行数须等于覆盖前的目标行数，INSERT 影响行数须与暂存行数相同，发布后总行数还须符合覆盖或追加预期，否则回滚，避免触发器忽略记录后仍报告完整成功。`rows` 返回已提交的实际发布行数，不能在提交后重新计算源 DataFrame。

Spark 写入 MySQL、Doris 同样先完成独占暂存，再发布正式目标。`overwrite` 只替换数据，不能删除重建已有目标；字段集合或类型不匹配时拒绝，不通过隐式改表迁就结果。暂存失败不修改已有目标；暂存表不进入正式 outputs 或扫描范围。Worker 重试不提供行级去重，发布响应丢失不自动重试或宣称回滚成功。

MySQL 首期限定普通 InnoDB 表。已有目标在一个 SERIALIZABLE 事务内校验结构、核对行数并执行 DELETE + INSERT SELECT（覆盖）或 INSERT SELECT（追加），保留索引、约束、权限和注释。发布阶段按目标串行化 ADDP 写入；外部写入仍由数据库锁和事务裁决。生成列和自增列暂不支持，必须在修改目标前拒绝；外键、检查约束和触发器遵守数据库原生语义，约束拒绝时回滚目标事务，不承诺目标外触发器或非事务资源副作用的回滚。字符串可写入 CHAR/VARCHAR/TEXT，同族容量约束通过严格模式验证；数值类型、符号及 Decimal 精度和小数位必须匹配；时间列保留微秒精度，TIMESTAMP 使用统一 UTC JDBC 会话，DATETIME 对应无时区时间。新目标使用暂存表原子改名；DDL 与数据事务分开，不能用 TRUNCATE 或把 MySQL DDL 当作可回滚 DML。

Doris 表读取与暂存写入统一使用固定版本官方 Spark Doris Connector；表读取不得经 MySQL JDBC 推断字符串类型，保留原始字符串、合法尾随空格与 null，不用 trim 修补协议造成的定长填充。JDBC 仅承载发布控制 SQL。Doris 使用官方连接器写暂存表，连接的 FE HTTP 端口是 System Engine 部署事实，Driver 与 Executor 必须同时能访问 FE 和实际承载导入的 BE，不由 JDBC 端口猜测 HTTP 映射。连接器采用两阶段提交并核验可见性；JSON 暂存导入固定启用 `num_as_string=true`，按原始数字文本转换为目标列类型，禁止先经浮点数解析损失 bigint/Decimal 精度；正式发布使用 INSERT OVERWRITE SELECT（覆盖）或单条 INSERT INTO SELECT（追加），启用严格模式并核验提交状态。仅已确认可见的发布可报告成功并形成正式产物；已提交但尚不可见、状态未知和失败必须区分，不能盲目重放。首期限定与结果结构一致的普通 Duplicate Key OLAP 表；Unique/Aggregate 模型不得冒充普通追加语义。首次创建目标与覆盖已有目标分别验证，保留已有目标的模型、分区、分桶、索引与属性，不提供跨表事务或端到端恰好一次保证。

Spark `save` 声明 `write` 和 `ddl` 效果，因为暂存表和新目标涉及 DDL。Develop 按资源分别签发授权：输入引擎与 Spark 集群只读；表目标引擎要求 write + ddl；文件或对象目标引擎只要求 write。执行 Runtime 获得算子声明的效果全集。正式输出继续使用节点命名的 `metadata.outputs` 与 `produced_targets`，由 Develop 触发 Meta 异步扫描和血缘采集，扫描失败不得改写已提交的保存事实。

**错误响应**：
```json
{
  "status": "failed",
  "error": "工作流执行失败: 除数不能为零",
  "error_code": "EXECUTION_FAILED",
  "details": "任务 task3 执行失败"
}
```

### 调用单个算子

单算子调用用于业务模块受控消费扩展引擎能力，例如 Manager 触发 `raster_to_cog` 生成 COG 结果。它不是任务执行入口，不创建 Develop 工作流任务，不进入 Orchestrator 编排，也不进入 Monitor 通用任务监控；调用方模块必须负责自己的领域状态、审计和失败处理。

只有同一运行时返回的算子元数据 `execution_modes` 显式包含 `direct` 时，才允许调用。Math 的 `add` 只声明 `workflow`，通过该接口调用应返回 HTTP `403` 和 `DIRECT_NOT_SUPPORTED`。

**请求封装示意**：

```http
POST /api/operators/<name>/invoke
Content-Type: application/json

{
  "params": {}
}
```

`<name>` 和 `params` 必须按该运行时的算子元数据填写；空对象仅展示请求封装，不代表实际算子无必填参数。资源参数的派生遵循 [Develop 资源与执行契约](Develop资源与执行.md#算子契约分层)。

对 `spark_workflow`，请求必须携带顶层 `engine_id`，指向真实 `engine_type=spark` 的通用引擎资源；Develop/Manager 等调用方必须由 `execution_config.engine_specific.spark_cluster_id` 或等价领域配置映射得到，不能和工作流运行时实例 ID 或数据源 locator 中的存储引擎 ID 混用。其他 Workflow Runtime 的 direct 请求不得携带该字段。

GeoPython 栅格 direct 调用还需由 owner 提供顶层 `tenant_id`，用于额度计数；租户身份与授权边界见[栅格资源准入](栅格算子契约.md#geopython-栅格资源准入)。

成功响应中的 `result` 按实际算子定义返回，`execution_time_ms` 表达执行耗时。`execution_id` 是可选字段：同步执行且调用方模块自行管理领域状态时可以不返回；如果引擎内部为 direct 调用生成了运行时执行记录，可以返回该 ID 供诊断使用。完整请求与响应字段见 [OpenAPI](../../../engines/docs/workflow-engine-api-v1.yaml) 的 `OperatorInvokeRequest` / `OperatorInvokeResponse`。

声明支持二进制 direct 的算子还可以接收与 `params` 并列的 `binary_payload`：

| 字段 | 含义 |
| --- | --- |
| `content_type` | 载荷 MIME 类型，按算子声明填写 |
| `encoding` | 载荷编码，按算子声明填写 |
| `name` | 载荷名称或输入端口名 |
| `data` | 真实二进制数据的标准 base64 编码，解码后必须符合声明的载荷格式 |
| `metadata` | 批元数据，按算子的二进制契约填写 |

二进制载荷可用于几何批、Arrow 记录批等场景。运行时必须按二进制语义处理，不得再把几何数组展开成 JSON records。对于几何坐标转换等批处理算子，载荷必须只包含 geometry 列及其批元数据，不得混入其他属性字段。

### 查询执行状态

**请求**：
```http
GET /api/executions/uuid-1234-5678
```

**响应**：
```json
{
  "status": "success",
  "execution_id": "uuid-1234-5678",
  "result": 60,
  "all_results": {
    "task1": 30,
    "task2": 60
  },
  "progress": 100,
  "execution_time_ms": 12.34,
  "message": "执行完成"
}
```

状态查询响应只使用一个 `status` 表达运行时本地执行状态，不得再同时返回 `task_status`。同步执行的引擎也必须只对已知的本地 `execution_id` 返回状态；未知执行 ID 必须返回 404 和 `EXECUTION_NOT_FOUND`。

`POST /api/workflow` 可以同步完成并返回 `status="success"` / `status="failed"`，也可以仅完成受理并返回 `status="running"` / `status="pending"`。只要返回了 `running` 或 `pending`，运行时必须已经创建可查询的本地 `execution_id`，并保证 `GET /api/executions/{execution_id}` 能持续返回该执行的最新状态，直到进入 `success`、`failed` 或 `cancelled` 等终态。进入 ADDP 任务体系的调用方不得把 `running` / `pending` 当作成功；Develop 后端必须通过 `WorkflowRuntimeProvider.GetExecutionStatus()` 轮询到终态后，才更新 `common.task_executions` 的最终状态。

## 标准错误码

扩展工作流引擎返回失败时应使用对应的 HTTP 4xx/5xx 状态码，并在响应体中提供 `status="failed"`、`error_code` 和 `error`。调用方仍必须把标准响应体中的 `status="failed"` 或 `status="error"` 视为失败，即使运行时错误地返回了 HTTP 2xx；不得仅凭 HTTP 2xx 将工作流或 direct 调用标记为成功。

| 错误码 | 说明 | HTTP 状态码 |
|--------|------|------------|
| `OPERATOR_NOT_FOUND` | 算子不存在 | 404 |
| `DIRECT_NOT_SUPPORTED` | 算子未声明 `execution_modes: ["direct"]`，不允许单算子调用 | 403 |
| `EXECUTION_NOT_FOUND` | 执行记录不存在 | 404 |
| `INVALID_PARAMS` | 参数错误 | 400 |
| `EXECUTION_FAILED` | 执行失败 | 500 |
| `WORKFLOW_INVALID` | 工作流定义无效 | 400 |
| `INTERNAL_ERROR` | 内部错误 | 500 |

---
