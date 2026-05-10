# Common 共享后端模块说明

## 模块定位

`common/` 是 ADDP Go 后端共享库，承载跨模块复用的 API 响应、客户端、配置、模型、执行记录、资源定位、调度、空间处理、SQL 构建、存储和通用工具。跨模块重复逻辑应优先抽取到这里。

## 重要包

```text
common/
├── api/            # 统一响应、错误和 handler 辅助
├── client/         # System、Meta、Asset、Service 等模块客户端
├── config/         # .env 加载和服务配置
├── jsonmap/        # decoded JSON map 通用读取工具
├── format/         # 文件格式、类型信息、格式信息、parser / analyzer
├── models/         # 通用模型、能力声明、统一执行记录
├── repository/     # 通用仓储，含 task_execution_repository
├── resource/       # 资源定位符、资源树、资源读取和对象存储 ResourceReader 适配
├── scheduler/      # 统一 Cron 调度
├── spatial/        # CRS、MVT、WKB、空间转换、PostGIS 空间 SQL 表达式
├── sqldialect/     # 跨 SQL 引擎的标识符引用、分页、基础 SELECT/COUNT
├── duckdb/         # DuckDB 联邦查询能力
└── utils/          # 加密、脱敏、端口、时区等工具
```

## 开发规则

- 新增共享能力必须保持模块边界清晰，避免把某个业务模块的私有逻辑沉淀到 `common/`。
- `common/jsonmap` 只提供通用 JSON map 读写 helper，不承载 `meta_item.attributes` 规范语义。
- `common/format` 只提供通用格式、type info / format info、parser / analyzer 能力；Meta item 识别、claims / exclusive、`meta_item.full_name` 决策和 attributes 落库构造属于 Meta 模块。
- API 响应优先复用 `common/api`，执行记录优先复用 `common/models.TaskExecution` 和 `common/repository.TaskExecutionRepository`。
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
