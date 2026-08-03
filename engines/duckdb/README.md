# DuckDB Federated Query Runtime

`engines/duckdb` 是 ADDP 的内置联邦查询计算引擎。Develop 和 Service 只通过
`FederatedQueryRuntimeProvider` 调用该 Runtime，不在各自进程内链接 DuckDB。

Runtime 使用 `addp-duckdb` Service Principal 消费 Execution Authorization，并按授权中的
Source Engine ID 从 System 获取短期连接信息。当前可挂载 PostgreSQL、MySQL、MinIO 和 S3；
对象表读取格式只支持 Parquet。

## 扩展生命周期

固定依赖 `httpfs`、`postgres_scanner`、`mysql_scanner` 和 `spatial`。开发环境由
`scripts/dev/start.sh` 在 Runtime 启动前准备并验证扩展；生产镜像在 builder 阶段下载到
BuildKit 扩展缓存，并随镜像写入 `/opt/addp/duckdb/extensions`。Builder 和 Runtime 固定使用
Debian Bookworm，保证 DuckDB CGO 链接的 glibc 版本一致。请求处理阶段关闭 DuckDB 自动安装
和自动加载，不下载扩展。调用方通过 `QueryOptions.Spatial` 声明当前查询需要空间类型或函数；
Runtime 只在这类独立查询会话锁定配置前从上述目录显式加载 `spatial`，普通查询不承担该加载成本。

## 本地运行

```bash
bash scripts/dev/start.sh -duckdb
./scripts/dev/restart.sh -duckdb
```

默认健康检查地址为 `http://localhost:8104/health`。

本地二进制默认不设置 `DUCKDB_SOURCE_LOOPBACK_HOST`，因此 System Engine 中的 `localhost`、
`127.0.0.1` 和 `::1` 保持宿主机语义。
开发启动脚本只复用自身 PID 文件管理的本地 Runtime，不启动或复用 Compose 中的
`duckdb-engine`；镜像实例占用 `8104` 时会明确报错，必须先停止镜像实例。

## 镜像运行

DuckDB 与 Jupyter、PointCloud 等内置计算引擎一样，由根 `docker-compose.yml` 同时声明
`build` 和 `image`。镜像启动只使用这一条 Compose 路径，Compose project 固定为
`addp-app`：

```bash
docker compose -f docker-compose.yml build duckdb-engine
docker compose -f docker-compose.yml up -d duckdb-engine
docker compose -f docker-compose.yml ps duckdb-engine
```

不要使用独立 `docker run` 创建 DuckDB Runtime；否则容器不会进入 `addp-app`，也无法复用
统一的依赖、网络、健康检查和停止流程。

根 Compose 固定设置 `DUCKDB_SOURCE_LOOPBACK_HOST=host.docker.internal`，并通过
`host-gateway` 解析该地址。Runtime 只在执行期 Engine 副本中映射 PostgreSQL、MySQL 的
`host` 和 MinIO、S3 的 `endpoint`；System 登记的 Engine 连接事实不会被改写。

## 验证

```bash
cd engines/duckdb
go test ./...
```
