# Common-Python 共享模块说明

## 模块定位

`common-python/` 为 ADDP Python 后端和 Python Workflow Runtime 提供共享 HTTP 客户端、协议执行核心和工具，减少跨模块与跨运行时重复实现。

## 重要目录

以下按职责列出主要入口，不穷举文件；使用示例和验证入口见 [README](README.md)。

```text
common-python/
├── addp_common/
│   ├── __init__.py         # 包版本事实源
│   ├── auth.py             # 权威 AuthContext 解析
│   ├── oauth.py            # CLI OAuth、Keychain 与跨进程锁
│   ├── cli.py              # addp 命令入口与 ToolExecutor Adapter
│   ├── client/             # Owner SDK、服务令牌及注册客户端
│   ├── resources/          # 资源事实数据模型
│   ├── tools/              # Tool Manifest、校验与唯一执行映射
│   ├── workflow_runtime/   # 通用工作流协议、执行与就绪核心
│   ├── workflow_access.py  # 工作流源访问与目录暂存
│   ├── notebook/           # Notebook 引擎发现门面
│   ├── module_lifecycle.py # Python Backend 健康、就绪与注册生命周期
│   ├── runtime_logging.py  # 服务正文日志 JSON 出口
│   ├── http_observation.py # HTTP 观测共用传输归一化
│   ├── httpmetrics/        # HTTP 指标与受保护来源
│   ├── httptrace/          # HTTP Trace 与有界导出
│   ├── inference_langchain.py # Inference 的可选 LangChain 适配
│   └── raster_mosaic.py    # 栅格镶嵌 Manifest 与来源索引
├── tests/                  # 共享库回归与 CLI 产品 E2E 用例
├── pyproject.toml          # 依赖、包构建与 addp entry point
├── README.md               # 使用与标准验证入口
└── common-python实施报告.md # 产品门禁断言与未覆盖边界
```

客户端导出入口见 [`client/__init__.py`](addp_common/client/__init__.py)，包含 Service Consumer、Inference、Ontology、Transfer 等 SDK；服务令牌、模块实例登记和 Runtime 引擎登记分别见 [`service_token.py`](addp_common/client/service_token.py)、[`module_registry.py`](addp_common/client/module_registry.py) 和 [`runtime_registration.py`](addp_common/client/runtime_registration.py)。栅格策略客户端 [`raster_policy.py`](addp_common/client/raster_policy.py) 的访问身份限定为 Platform Service OAuth，不因位于共享库而扩大权限。

Tool 契约与执行映射分别定位到 [`manifest.json`](addp_common/tools/manifest.json) 和 [`executor.py`](addp_common/tools/executor.py)；目录导航不代表功能或运行验收通过，覆盖范围与结果认定见[验证](#验证)。

## 开发规则

- 新增 Python 服务间调用客户端时，优先扩展 `addp_common/client/`，不要在 `agent`、`copilot` 中重复实现。
- `addp_common.client.ServiceConsumerClient` 是 Python 消费 `addp.service_consumer/v1` 的唯一客户端；只通过 Consumer Catalog、Descriptor 和 Descriptor 声明的 query operation 读取数据，不读取 Service 管理 DTO，不硬编码业务字段或数据类型。
- 用户交互请求使用短期 User Access Token；服务间请求使用各模块独立 Confidential OAuth Client 通过 Client Credentials Grant 获取短期 Service Access Token，业务请求只发送 `Authorization: Bearer`。
- 不得新增或恢复 `internal_api_key`、`X-Internal-API-Key`、`X-Tenant-ID`、用户 Token 代传或其他服务间认证双轨。
- 用户 `user_token` 只能通过 `addp_common.auth.resolve_authorization_context()` 调用 System AuthContext API 解析；Python 模块不自行解析 JWT。
- 客户端 URL 与 API 路径要以各模块当前 `CLAUDE.md`、路由和 Swagger 为准。
- `tools/manifest.json` 是 AI Tool 契约事实源，`ToolExecutor` 是 Manifest 到 SDK 的唯一执行映射。
- `resource.facts.get` 的 `2.0.0` 输入契约只接受 Owner 返回、带 `item_id` 且不含 `node_id` 的业务数据项 locator。Manifest 在签发委托及调用 Manager 前检查身份结构；不以 `type` 白名单猜测 node/item，不改写 locator，真实身份与权限仍由 Owner 判定。详细要求见[资源事实规范](../docs/spec/addp智能体Tool开放规范.md#45-resourcefactsget)。
- `engine.list` 只消费 `SystemClient.list_engine_summaries()` 的五字段投影，all/workflow 共用实现；生命周期来自 `lifecycle_state`，不带连接信息和完整能力声明。System 管理数组仍由 `list_engines()` 原样消费。已删除旧 `get_workflow_engines()` 与 `is_active` 路径。
- `resource.children.list` 的根节点与指定父节点发现统一见[目录发现规范](../docs/spec/addp智能体Tool开放规范.md#44-resourcechildrenlist)；只枚举已验证范围的直接子资源。父 locator 是 discovery scope，不是输入资源或执行目标；具体数据项继续通过 `resource.facts.get` 确认身份与结构事实，不读取原始行。
- `data.preview` 只用于明确请求查看样本，不代替生成前的 Schema 校验，也不作为扫描事实缺失时的兜底，职责见[结构事实与样本读取规范](../docs/spec/addp智能体Tool开放规范.md#45-resourcefactsget)。目录发现、结构事实和样本读取的 Owner、Permission 与委托 Scope 按[Tool 集合](../docs/spec/addp智能体Tool开放规范.md#34-当前-tool-集合)分别核对；静态映射、目录可见或结构确认不能代替样本读取与后续执行的授权和[实际验收](#验证)。
- `ToolExecutor` 每次调用必须使用源 User Access Token 向 System 申请绑定 owner audience、稳定 Tool Scope、AgentRun 和 ToolCall 的短期 Delegated Access Token；owner SDK Client 只能接收该委托令牌。
- `addp` CLI 和 Agent Tool Provider 只能作为 `ToolExecutor` 的薄 Adapter，不得直接发 HTTP。
- CLI stdout 必须是单个紧凑 JSON，日志只写 stderr，并保持稳定 exit code。
- CLI 登录方式、公共客户端、凭据存储与跨进程锁、Context 选择和状态语义遵循[CLI 产品契约](<../docs/spec/addp OAuth授权规范.md#71-正式-addp-cli-产品契约>)，操作说明统一见[README 认证方式](README.md#认证方式)。
- CLI 不接受 `--token`、`ADDP_TOKEN` 或其他手工 User Access Token 注入路径；每次 Tool 调用都必须走 Keychain 刷新和跨进程锁。
- CLI 支持矩阵与各平台真实凭据后端的证据范围统一见[README 认证方式](README.md#认证方式)；产品 E2E 独有断言和未覆盖项见实施报告的[发布门禁](common-python实施报告.md#发布门禁)与[延期边界](common-python实施报告.md#延期边界)，结果认定沿用[验证](#验证)。
- 修改公共客户端后，至少验证直接使用它的 Python 模块。
- `workflow_runtime` 只承载 `addp.workflow/v1` 的通用 DAG、引用、execution 状态及整体就绪响应，不依赖 Flask、GeoPandas、Spark、PDAL 或三维转换器。健康响应统一汇总结构化本地依赖，required 条件全部可用时为 HTTP 200 + healthy，否则为 HTTP 503 + unhealthy；实例身份登记与就绪独立。
- 各 Python Workflow Runtime 使用公共核心后，必须删除本地重复实现，不保留兼容执行路径。
- `workflow_access` 的源对象键、目录前缀和成员名称按原始值读取，不裁剪名称中的空格。目录暂存只接受当前前缀内可安全表示的相对成员；先核对完整返回集合，再下载，不把越界成员按 basename 改名兜底。此项是共享 I/O 契约，不替代各 Owner 的源授权。

## 验证

开发环境、依赖准备与常用验证统一见 [README 开发与验证](README.md#开发与验证)；从仓库根目录按[测试标准入口](../docs/spec/addp测试与验收规范.md#三标准入口)选择实际改动范围。

共享库全量回归、Agent 离线评测、授权检查及受影响 owner 的覆盖分工见[Tool 最低验证](../docs/spec/addp智能体Tool开放规范.md#八变更与验证)。CLI 产品的 wheel、全新 venv、pipx 生命周期、真实 macOS Keychain 门禁与独立 System IAM T2 的范围及前置条件，统一沿用上述 README。

导入与最小调用仅用于环境定位，不能替代完整门禁。共享库回归和 Agent 离线结果不证明真实路由、跨模块或模型调用可用；CLI 产品认证与服务端事务验证分别认定。缺少依赖、未运行、失败和未覆盖项分别记录，不计为通过。

## 相关文档

- [README：客户端、资源发现与验证](README.md)
- [Common-Python 当前实施状态](common-python实施报告.md)
- [Tool 开放规范](../docs/spec/addp智能体Tool开放规范.md)
- [智能体评测规范](../docs/spec/addp智能体评测规范.md)
- [OAuth 授权规范](<../docs/spec/addp OAuth授权规范.md>)
- [Agent 模块](../agent/CLAUDE.md)
- [Copilot 模块](../copilot/CLAUDE.md)
