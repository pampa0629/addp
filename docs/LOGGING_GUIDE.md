# 日志体系指南

本文描述仓库内统一日志方案的使用方式以及集中式采集的推荐部署策略。

## 统一日志库

- 新增 `common/logger`，基于 Go 标准库 `log/slog` 实现结构化输出。
- 支持 `json`（默认）与 `text` 两种格式，环境变量控制：
- `LOG_LEVEL`：`debug` / `info` / `warn` / `error`
- `LOG_FORMAT`：`json` / `text`
- `LOG_ADD_SOURCE`：是否输出调用源信息（`true`/`false`）
- `LOG_FILE`：日志落盘路径（为空则写入 stdout，Meta 默认写入 `logs/meta-backend.log`，可自定义成其他文件；目录会自动创建）
- 各服务启动时在加载配置后调用 `logger.Init`，即可统一输出到 `stdout`。
- 服务内部通过 `logger.With(...).Info/Warn/Error` 追加上下文字段，例如 `tenant_id`、`resource_id`、`scan_log_id`。

## 框架集成

- Gin 路由替换为自定义中间件，按状态码分别输出 Info/Warn/Error，并记录访问路径、耗时、客户端 IP。
- Gorm 使用自定义 `gormSlogLogger`，支持慢查询告警（默认阈值 200ms）与结构化 SQL 记录。
- 资源、扫描等核心服务在关键步骤补充结构化日志，方便关联扫描日志表与运行时日志。

## 集中式日志平台

- 推荐部署 ELK（Elasticsearch + Logstash + Kibana）或 Loki + Grafana 方案，均可通过容器快速落地。
- 采用容器部署便于与现有 Docker Compose/容器编排体系集成，统一由 `stdout`/`stderr` 收集。
- 若选用 ELK：
  1. Filebeat/Vector 等 Agent 监听服务容器的标准输出或宿主机日志目录。
  2. Logstash 做解析与清洗，将字段（如 `tenant_id`、`resource_id`、`scan_log_id`）映射到索引。
  3. Kibana 配置 Saved Search/告警规则，用于排障和错误监控。
- 若选用 Loki：
  1. Promtail 收集容器日志并打上模块、环境等标签。
  2. 在 Grafana 中以 LogQL 查询，配合面板和告警通道（Webhook、钉钉等）。

## 运维建议

- 将日志保留策略、索引生命周期等在集中平台中配置，避免索引无限增长。
- 在非生产环境可将 `LOG_LEVEL` 设置为 `debug`，生产环境建议 `info` 并依赖集中平台做细粒度查询。
- 审计日志与运行日志可通过标签区分，便于权限控制与合规审计。
