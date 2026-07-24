# Common 共享后端模块说明

## 模块定位

`common/` 是 ADDP Go 后端共享库，承载跨模块复用的 API 响应、客户端、配置、模型、统一执行记录、内容 I/O、目录视图、调度、空间处理、SQL 构建、存储和通用工具。跨模块重复逻辑应优先抽取到这里。

## 重要包

```text
common/
├── api/            # 统一响应、错误和 handler 辅助
├── authorization/  # Permission/内置 Role Manifest Schema、仓库聚合 CLI、严格校验和共享授权契约
├── client/         # System、Meta、Asset、Service 等模块客户端
├── middleware/auth/ # System AuthContext 消费、Gin 上下文注入和租户隔离 helper
├── config/         # .env 加载和服务配置
├── resourcetree/    # Meta catalog / item 事实到资源树视图的投影和路径定位纯转换
├── contentio/      # 基于 Go io 的内容 Ref、Reader、Writer、Lister、RangeReader
├── engine/contentadapter/ # engine provider 到 contentio 的适配
├── jsonmap/        # decoded JSON map 通用读取工具
├── execution/      # common.task_executions 统一执行记录模型、仓储和迁移入口
├── format/         # 文件格式、类型信息、格式信息、parser / analyzer
├── models/         # 通用模型、能力声明和跨模块 DTO / 值对象
├── taskprovider/   # TaskProvider capabilities v1 解析和契约校验
├── repository/     # 通用数据库初始化和基础仓储错误映射
├── scheduler/      # 统一 Cron 调度
├── spatial/        # CRS、MVT、WKB、空间转换、PostGIS 空间 SQL 表达式
├── sqldialect/     # 跨 SQL 引擎的标识符引用、分页、基础 SELECT/COUNT
├── duckdb/         # DuckDB 联邦查询能力
└── utils/          # 加密、脱敏、端口、时区等工具
```

## 开发规则

- 新增共享能力必须保持模块边界清晰，避免把某个业务模块的私有逻辑沉淀到 `common/`。
- `common/authorization` 只提供 Permission/内置 Role Manifest Schema、解析/校验、确定性 Catalog Report、发布期聚合 CLI 和共享授权类型；业务 Permission 内容必须留在各 owner 的 `authorization/permissions.yaml`，产品内置 Role 内容必须留在 `system/authorization/builtin_roles.yaml`，不得建立 common 中央业务清单。
- `common/jsonmap` 只提供通用 JSON map 读写 helper，不承载 `meta_item.attributes` 规范语义。
- `common/format` 只提供通用格式、type info / format info、parser / analyzer 能力；Meta item 识别、claims / exclusive、`meta_item.full_name` 决策和 attributes 落库构造属于 Meta 模块。
- `common/contentio` 只表达内容定位和 I/O，不依赖 engine，不解析 format，不返回上层 DTO。
- `common/engine/contentadapter` 负责把 engine content provider 适配为 `contentio.Reader` / `Writer`。
- `common/resourcetree` 负责把 Meta 已落库的 catalog / item 事实投影为跨模块资源树视图，并提供 `ResourceLocator` / provider `CatalogPath` 的纯转换能力。
- `common/resourcetree` 不持有 System / Meta client，不主动读取远程服务，不处理租户权限、token、降级策略、扫描或内容读取。
- `common/resourcetree` 中 attributes helper 只服务 `TreeNode.Metadata` 展示摘要，不作为通用 attributes 规范 API，也不写入持久 attributes。
- `common/taskprovider` 只承载 TaskProvider capabilities 的纯解析和规范校验，不访问 System 注册表，不调用 owner 模块，不处理执行调度。
- `common/client` 只放跨服务 HTTP/API 客户端，不作为 infra PostgreSQL `common` schema 的读写入口。
- `common` schema 中的共享表应按领域归入 `common/<domain>`，由领域包提供模型、仓储和 `EnsureStore`；执行记录必须复用 `common/execution.TaskExecution`、`common/execution.TaskExecutionRepository` 和 `common/execution.EnsureStore`。
- API 响应优先复用 `common/api`。
- 用户 Bearer Token 统一调用 System `/api/v1/system/auth/context`；业务模块不通过 `/users/me` 验证 Token，不自行解析 JWT。
- `common/sqldialect` 只承载跨 SQL 引擎的基础方言差异；PostGIS 等空间扩展能力归入 `common/spatial`。
- 空间能力不要默认几何字段名为 `geom`，应通过元数据或调用方参数传入。
- 修改 `common/` 后通常需要 `./scripts/dev/restart.sh -all` 验证受影响模块。

## 验证

```bash
cd common && go test ./...
./scripts/dev/restart.sh -all
```

## 相关文档

- `common/README.md`
- `common/scheduler/README.md`
- `common/format/README.md`
- `common/spatial/README.md`
- `docs/concepts/addp共享模块介绍.md`
