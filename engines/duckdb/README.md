# DuckDB Federated Query Runtime

`engines/duckdb` 是 ADDP 的内置联邦查询计算引擎。Develop 和 Service 只通过
`FederatedQueryRuntimeProvider` 调用该 Runtime，不在各自进程内链接 DuckDB。

Runtime 使用 `addp-duckdb` Service Principal 消费 Execution Authorization，并按授权中的
Source Engine ID 从 System 获取短期连接信息。当前可挂载 PostgreSQL、MySQL、MinIO 和 S3；
对象表读取格式只支持 Parquet。Runtime 解析查询实际引用的对象表，在准备阶段列出目录下本次匹配的 Parquet 文件。读取表达式和会话的允许路径均使用这份文件清单；准备后新增的文件由下次查询处理。

## 扩展生命周期

固定依赖 `httpfs`、`postgres_scanner`、`mysql_scanner` 和 `spatial`。开发环境由
`scripts/dev/start.sh` 在 Runtime 启动前准备并验证扩展；生产镜像在 builder 阶段构建 PostgreSQL 扫描器、下载其余三个官方扩展，并随镜像写入 `/opt/addp/duckdb/extensions`。Builder 和 Runtime 固定使用
Debian Bookworm，保证 DuckDB CGO 链接的 glibc 版本一致。请求处理阶段关闭 DuckDB 自动安装
和自动加载，不下载扩展。调用方通过 `QueryOptions.Spatial` 声明当前查询需要空间类型或函数；
Runtime 只在这类独立查询会话锁定配置前从上述目录显式加载 `spatial`，普通查询不承担该加载成本。

### PostgreSQL 普通表扫描器的构建验证

ADDP 的补丁针对 DuckDB 1.4.5 发布树指定的 `duckdb-postgres` 提交
`b9fce43bc5d36bc6db70844f28b7b146e756eb22`。构建对 DuckDB、扫描器及其 libpq
源码归档逐一验证 SHA-256。使用 CMake、Ninja、C/C++ 编译器和 OpenSSL 开发库，在目标平台运行：

```sh
engines/duckdb/postgres-scanner/build.sh /tmp/addp-scanner-build /tmp/addp-scanner-out
```

标准输出给出本平台本次构建的 SHA-256，供 Go 链接器固定到 Runtime 和启动验证工具。加载时核对私有副本，恢复签名检查后再挂载源库。输出文件是该平台的 `postgres_scanner.duckdb_extension`。选项 `pg_only_named_table` 在表绑定时确定，
具名表扫描保留原生投影、筛选与分页。发布验收须核对 Runtime 对产物的信任、工作台按原用户执行的
DataItem 裁决，以及已发布服务原有的表读取范围。构建命令只写入指定输出目录。

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

DuckDB 与 Jupyter、PointCloud 等内置计算引擎一样，由根 `docker-compose.runtimes.yml` 同时声明
`build` 和 `image`。镜像启动只使用这一条 Compose 路径，Compose project 固定为
`addp-runtimes`：

```bash
docker compose -f docker-compose.runtimes.yml build duckdb-engine
docker compose -f docker-compose.runtimes.yml up -d duckdb-engine
docker compose -f docker-compose.runtimes.yml ps duckdb-engine
```

不要使用独立 `docker run` 创建 DuckDB Runtime；否则容器不会进入 `addp-runtimes`，也无法复用
统一的依赖、网络、健康检查和停止流程。

根 Compose 固定设置 `DUCKDB_SOURCE_LOOPBACK_HOST=host.docker.internal`，并通过
`host-gateway` 解析该地址。Runtime 只在执行期 Engine 副本中映射 PostgreSQL、MySQL 的
`host` 和 MinIO、S3 的 `endpoint`；System 登记的 Engine 连接事实不会被改写。

## 验证

```bash
# 在仓库根目录运行完整 owner 门禁
make test-module MODULE=engines

# 仅调试 owner 的 Go T1，不计为完整模块验证
make test-module MODULE=engines GO_ONLY=1
```
