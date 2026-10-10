# Spark 工作流 空间计算引擎

Spark 工作流 Engine 是 ADDP 平台的分布式空间计算引擎,基于 Apache Spark 和 Apache Sedona (原 GeoSpark) 构建,支持大规模空间数据处理和分析。

## 核心特性

- **分布式计算**: 支持 TB 级空间数据处理,自动并行化
- **Sedona 空间算子**: 提供核心空间算子 (buffer, intersection, spatial_join 等)
- **统一存储访问**: 支持数据库 (PostgreSQL/MySQL/Doris)、对象存储 (S3/MinIO/HDFS)、湖仓 (Iceberg/Delta/Hudi)，以及 Elasticsearch 具体索引只读批量加载
- **动态 Spark 资源**: 用户注册多个 Spark 集群,工作流执行时选择
- **DAG 工作流**: 拓扑排序 + DataFrame 内存传递,最小化序列化开销
- **与 GeoPython Workflow 互补**: 快速原型用 GeoPython Workflow,生产大规模用 Spark

## 目录结构

```
engines/spark-workflow/
├── api_server.py           # Flask API Server (端口 8098)
├── runtime_server.py       # 原生开发与产品容器共用的 HTTP 启动和就绪后注册
├── workflow_engine.py      # 公共 WorkflowRunner 的 Spark 领域算子适配
├── spark_connector.py      # 动态 SparkSession 管理器
├── elasticsearch_adapter.py # ES 具体索引 Mapping、数组和只读访问
├── spark_dependencies.py   # 固定 ES Spark JAR 下载和 SHA256 校验
├── storage_adapters.py     # 统一存储访问适配器
├── operators/              # 算子实现与 Pydantic 元数据定义
├── operator_metadata.py    # 公开算子元数据出口 (供前端使用)
├── requirements.txt        # Python 依赖
├── start.sh                # 启动脚本
└── README.md               # 本文件
```

## 算子列表

### 1. 数据 I/O (5个)
- `load` - 数据加载 (数据库/文件/湖仓/SQL/Elasticsearch 索引)
- `save` - 数据保存 (数据库/文件)
- `preview` - 数据预览
- `cache` - 内存缓存
- `persist` - 持久化

### 2. Sedona 空间算子 (7个)
- `buffer` - 缓冲区分析
- `centroid` - 质心计算
- `intersection` - 几何相交
- `union` - 几何合并
- `spatial_join` - 空间连接 (核心)
- `distance` - 距离计算
- `transform` - 坐标转换

### 3. 数据转换 (5个)
- `select` - 选择列
- `filter` - 条件过滤
- `add_column` - 添加列 (支持SQL表达式)
- `rename_column` - 重命名列
- `drop_column` - 删除列

### 4. 聚合分析 (2个)
- `group_by` - 分组聚合
- `join` - 表连接

### 5. SQL 查询 (2个)
- `sql` - 自由SQL查询
- `create_temp_view` - 创建临时视图

## 快速开始

Spark Workflow 本地开发统一使用宿主机 Python 3.11/3.12 虚拟环境和 OpenJDK 11，与 Business Spark Worker 保持 JVM 主版本一致。`start.sh -spark-workflow` 与 `restart.sh -spark-workflow` 共用唯一原生启动入口，按完整 requirements 和 editable `common-python` 同步依赖并执行 `pip check`。全套与局部重启均先通过 stop.sh 停止所选服务，再由 start.sh 完成 Java、Python、依赖和共享地址验证；准备失败的服务不启动，入口返回非零。HTTP 仅绑定 `127.0.0.1`，就绪必须同时验证原生 PID、监听归属和 HTTP 健康检查；`stop.sh` 按原生 PID 停止服务。macOS 使用 `host.docker.internal` 公布 Driver 与回环数据端点，需要在本机 `/etc/hosts` 配置 `127.0.0.1 host.docker.internal`；Docker Worker 保留 Docker 内置解析。该方式不依赖 Docker Desktop host networking。生产 Compose 和 Hosted 产品验收使用独立容器入口，仍验证同一应用的镜像默认启动命令。


Spark JDBC 的 schema 解析发生在 driver，分区读取和写入发生在 executor，因此两端必须使用同一个可达地址。当数据引擎连接地址是 loopback 时，Spark Workflow 只在构造 JDBC URL 时使用 `SPARK_WORKFLOW_SHARED_HOST`，System 中保存的连接配置不变；远程主机地址不会被改写。PostgreSQL JDBC URL 同时继承 System `connection_info.sslmode`。

Elasticsearch 的 `load` 使用索引 locator 和独立 Spark 集群；Develop 按当前用户授权派生连接与单段索引名，任务不保存连接或 `index`。首版仅普通具体索引的 HTTP Basic 只读批量访问，HTTPS 显式拒绝。Driver 和 Executor 均直接访问源端点，不继承系统 HTTP/SOCKS 代理；必须能访问同一端点，回环地址使用 `SPARK_WORKFLOW_SHARED_HOST`。公开 `array_fields` 显式声明 Mapping 无法区分的数组字段，nested 无需重复声明。不支持别名、data stream、隐藏索引、DSL、流式、写回，也不承诺各次 action 共享同一个 PIT。Spark 内部保留 bigint，JSON 摘要超出 JavaScript 安全整数范围时转成十进制字符串。固定官方 `elasticsearch-spark-30_2.12:9.5.4` 完整 JAR 校验 SHA256，构建时缓存，通过 `PYSPARK_SUBMIT_ARGS --jars` 在 Spark 提交阶段与 Sedona/JDBC Maven 依赖合并分发，避免该制品 POM 引入另一版 Spark。Python Session 不设置 `spark.jars`，以免覆盖提交阶段已解析的完整依赖列表。生产 Runtime、镜像构建和分布式 T2 共用同一个依赖配置入口；T2 必须同时核对实际 JAR 分发列表和 Worker 上的 Sedona/ES 计算。

最小验证：`make test-spark-workflow`、`make test-common-elasticsearch-unit`、`make test-common-elasticsearch`；正式消费验收使用既有 Hosted T4 `elasticsearch-consumer-flow`，同时核对 Console、Runtime 状态和真实 Worker。

保存到 PostgreSQL 时，普通表和空间表统一通过 Worker JDBC 写入同 schema 的唯一暂存表。Runtime 按 DataFrame schema 识别 Geometry 列并以 EWKT 暂存，在发布事务中恢复 PostGIS geometry。已有目标的覆盖保存只 DELETE + INSERT，保留结构、约束、索引、权限和注释；追加只 INSERT。字段、类型或约束不匹配时回滚并清理暂存表，返回实际提交行数；暂存表不作为工作流产物暴露。表目标需要 write + ddl 授权；详细类型与事务边界见工作流计算引擎接口规范。

### 1. 配置与启动

部署配置使用仓库根目录 `.env`，不读取引擎目录中的 `.env`。关键配置为 `SYSTEM_URL`、`SPARK_WORKFLOW_PORT`（默认 8098）、独立 `SPARK_WORKFLOW_SERVICE_CLIENT_SECRET`，以及应用固定的 HDFS Simple 身份 `HADOOP_USER_NAME`。默认不设置 `SPARK_MODE`，计算使用用户选择的 Spark general Engine。

在仓库根目录执行：

```bash
bash scripts/dev/start.sh -spark-workflow
# 修改运行时代码后，使用同一产品构建和启动入口
./scripts/dev/restart.sh -spark-workflow
```

原生开发与产品默认入口 `python api_server.py` 使用一个 Gunicorn HTTP worker、四个请求线程及公共 `ExecutionRegistry`。健康检查成功后后台持续退避自注册，System 暂不可用不会阻塞监听。HTTP worker 与远端 Spark Worker 是不同职责；实际计算由注册集群的 Executor 执行。执行快照在 Runtime 重启后丢失，正式业务历史由平台保存。

工作流的 DAG、引用和 `$input` 使用公共 `WorkflowRunner`。算子输出按元数据声明的端口适配，`all_results` 的单端口节点直接返回结果，多端口节点返回端口对象，DataFrame 对外只返回最多五行的摘要。

服务启动后:
- API 端口: 8098
- 健康检查: http://localhost:8098/health
- 算子列表: http://localhost:8098/api/operators

## 使用示例

### 示例 1: 单算子 direct 调用

```bash
curl -X POST http://localhost:8098/api/operators/buffer/invoke \
  -H "Content-Type: application/json" \
  -d '{
    "engine_id": 34,
    "params": {
      "input_df": {...},
      "distance": 100,
      "geom_column": "geom"
    }
  }'
```

### 示例 2: 工作流执行

```bash
curl -X POST http://localhost:8098/api/workflow \
  -H "Content-Type: application/json" \
  -d '{
    "engine_id": 34,
    "workflow_def": {
      "tasks": [
        {
          "id": "load_poi",
          "operator": "load",
          "params": {
            "source_type": "table",
            "connection_info": {
              "engine_type": "postgresql",
              "host": "postgres",
              "port": 5432,
              "database": "addp",
              "user": "addp",
              "password": "secret"
            },
            "schema": "public",
            "table": "poi_data"
          },
          "depends_on": []
        },
        {
          "id": "buffer_analysis",
          "operator": "buffer",
          "params": {
            "input_df": {"$ref": "load_poi"},
            "distance": 100
          },
          "depends_on": ["load_poi"]
        },
        {
          "id": "save_result",
          "operator": "save",
          "params": {
            "input_df": {"$ref": "buffer_analysis"},
            "target_type": "table",
            "connection_info": {
              "engine_type": "postgresql",
              "host": "postgres",
              "port": 5432,
              "database": "addp",
              "user": "addp",
              "password": "secret"
            },
            "schema": "public",
            "table": "poi_buffer_result",
            "mode": "overwrite"
          },
          "depends_on": ["buffer_analysis"]
        }
      ]
    }
  }'
```

## 工作流定义格式

工作流定义只支持 `tasks` 数组格式，且 `tasks` 必须非空：

```json
{
  "engine_id": 34,
  "workflow_def": {
    "tasks": [
      {
        "id": "task1",
        "operator": "load",
        "params": {...},
        "depends_on": []
      },
      {
        "id": "task2",
        "operator": "buffer",
        "params": {
          "input_df": {"$ref": "task1"},
          "distance": 100
        },
        "depends_on": ["task1"]
      }
    ]
  }
}
```

上例展示的是 Spark Workflow runtime 收到的已预处理形态。用户和 AI 侧配置表、NFS 文件或对象存储输入/输出时使用 `locator` 或 `target_parent_locator + target_name`；Develop 后端会在调用 Spark Workflow runtime 前派生 `connection_info`、`schema/table` 或 `path`。Spark Workflow 顶层 `engine_id` 只绑定实际 Spark 通用引擎资源，不用于表达数据源。

## 核心设计

### 动态 Spark 资源管理

Spark 工作流 Engine 不内置 Spark 集群,而是动态连接到用户注册的 Spark 资源:

1. 用户在 System 模块注册 Spark 资源 (多个)
2. 创建 Spark Workflow 工作流时，Develop 前端用 `spark_cluster_id` 记录用户选择的 Spark 通用引擎资源
3. Develop 后端校验该资源为已启用的 `engine_type=spark`，调用运行时时映射为请求顶层 `engine_id`
4. Engine 通过 System API 获取资源配置,动态创建 SparkSession
5. 每个运行时 `engine_id` 对应一个 SparkSession (缓存复用)

```python
# spark_connector.py
connector = get_spark_connector()
spark = connector.get_or_create_session(engine_id=34)
```

### 统一存储访问

`StorageAdapter` 提供统一的数据加载/保存接口:

```python
# 加载: 数据库
df = StorageAdapter.load(spark, {
    "source_type": "table",
    "connection_info": {
        "engine_type": "postgresql",
        "host": "postgres",
        "port": 5432,
        "database": "addp",
        "user": "addp",
        "password": "secret"
    },
    "schema": "public",
    "table": "poi_data"
})

# 加载: S3
df = StorageAdapter.load(spark, {
    "source_type": "file",
    "connection_info": {
        "engine_type": "minio",
        "endpoint": "http://minio:9000",
        "access_key": "minioadmin",
        "secret_key": "minioadmin"
    },
    "path": "s3a://bucket/data.parquet",
    "format": "geoparquet"
})

# 加载: Iceberg
df = StorageAdapter.load(spark, {
    "source_type": "catalog",
    "catalog_name": "iceberg_catalog",
    "database": "analytics",
    "table": "poi"
})
```

### DAG 工作流执行

`SparkWorkflowRunner` 只注入可信 Spark 集群与租户上下文，并将领域算子结果映射到声明端口。公共 `WorkflowRunner` 校验 DAG、排序、解析嵌套 `$ref` / `$input`，在节点间直接传递 DataFrame；HTTP API 在执行结束后将结果投影为摘要。

```python
from workflow_engine import SparkWorkflowRunner

result = SparkWorkflowRunner(engine_id=34, tenant_id=7).execute(workflow_def, input_data={})
# result.final_result / result.all_results / result.task_order
```

## 与 GeoPython Workflow 的对比

| 特性 | GeoPython Workflow | Spark 工作流 Engine |
|------|------------------|---------------------|
| **适用场景** | 快速原型、探索分析 | 生产环境、大规模处理 |
| **数据规模** | < 10 GB | > 100 GB (TB 级) |
| **并行化** | 单机多进程 | 分布式集群 |
| **空间算子** | Python 空间分析算子 | Sedona 分布式空间算子 |
| **性能** | 单机 CPU/内存限制 | 集群自动扩展 |
| **部署方式** | 内置服务 (端口 8099) | 动态注册 (端口 8098) |
| **存储支持** | 数据库 + S3 | 数据库 + S3 + 湖仓 (Iceberg/Delta) |

**使用建议**:
- **原型开发**: 使用 GeoPython Workflow (快速迭代)
- **生产部署**: 使用 Spark (大规模稳定)
- **混合使用**: 在 Orchestrator 中跨引擎编排

## API 端点

### 健康检查
```
GET /health
```

### 获取算子列表
```
GET /api/operators
```

### 执行工作流
```
POST /api/workflow
Body: {"engine_id": 34, "workflow_def": {...}}
```

### direct 调用单个算子
```
POST /api/operators/{operator_name}/invoke
Body: {"engine_id": 34, "params": {...}}
```

### 查询执行状态
```
GET /api/executions/{execution_id}
```

## 故障排查

### 1. PySpark 导入失败

```bash
# 检查 PySpark 版本
python3 -c "import pyspark; print(pyspark.__version__)"

# 重新安装
pip install pyspark==3.5.0
```

### 2. Sedona 函数不可用

确保 Spark 配置中包含 Sedona 扩展:

```python
spark = SparkSession.builder \
    .config("spark.jars.packages", "org.apache.sedona:sedona-spark-shaded-3.5_2.12:1.5.3,org.datasyslab:geotools-wrapper:1.5.3-28.2,org.postgresql:postgresql:42.7.4,com.mysql:mysql-connector-j:8.4.0") \
    .config("spark.sql.extensions", "org.apache.sedona.sql.SedonaSqlExtensions") \
    .config("spark.serializer", "org.apache.spark.serializer.KryoSerializer") \
    .config("spark.kryo.registrator", "org.apache.sedona.core.serde.SedonaKryoRegistrator") \
    .getOrCreate()
```

### 3. 连接到远程 Spark 集群失败

检查 Spark 资源配置:
- 确保 Spark Thrift Server 正在运行
- 验证 host 和 port 可访问
- 检查 engine_id 是否正确

### 4. S3 访问失败

确保配置了 S3 凭证:
```python
spark.conf.set("spark.hadoop.fs.s3a.endpoint", "http://minio:9000")
spark.conf.set("spark.hadoop.fs.s3a.access.key", "minioadmin")
spark.conf.set("spark.hadoop.fs.s3a.secret.key", "minioadmin")
```

## 开发指南

### 添加新算子

1. 在对应的 `operators/*_operators.py` 中实现算子函数:
```python
def my_operator(input_df: DataFrame, param1: str, param2: int) -> DataFrame:
    """算子说明"""
    # 实现逻辑
    return result_df
```

2. 在同一文件中定义 `OperatorMetadata` 并注册到该分类的 `OPERATORS`:
```python
MY_OPERATOR_METADATA = OperatorMetadata(
    name="my_operator",
    category=OperatorCategory.DATA_TRANSFORM,
    description="我的算子",
    brief_description="执行自定义 Spark DataFrame 处理",
    overview="...",
    params=[
        OperatorParam(name="input_df", type="dataframe", required=True, description="输入 DataFrame"),
        OperatorParam(name="param1", type="str", required=True, description="参数 1"),
    ],
    use_cases=[...],
    notes=[...],
    input_desc="DataFrame",
    output_desc="DataFrame",
    workflow_example={
        "id": "my_operator_task",
        "operator": "my_operator",
        "params": {
            "input_df": {"$ref": "load_data"},
            "param1": "value"
        },
        "depends_on": ["load_data"]
    },
)

OPERATORS = dict([
    register_operator(MY_OPERATOR_METADATA, my_operator),
])
```

3. 在 `operators/__init__.py` 中导入算子函数和分类 `OPERATORS`。`operator_metadata.py` 是统一公开出口，不在其中手写单个算子的元数据。

`engine_id` 是 Spark Workflow 执行请求的顶层运行时绑定，由执行引擎注入到 `load`、`save`、`sql` 等内部函数；不要把它写入算子公开参数或 workflow example 的 `params`。

### 运行测试

确定性契约入口为 `make test-spark-workflow`（44 项），覆盖公共 WorkflowRunner、跨请求执行快照及真实 Gunicorn 的先监听、后注册行为；Hosted 编排入口为 `make test-hdfs-online-runner`（21 项）。产品镜像通过标准 `make build-images IMAGE_BUILD_ARGS="--services spark-workflow-engine --verify --jobs 1"` 构建验证。

2026-10-05，提交 `eccfaeeda07a51c471cde67d1fe1d5c624ea3a19` 的 [容器 HDFS T4](https://github.com/pampa0629/addp/actions/runs/37254858559) 完整通过。当次 AMD64 产品镜像使用默认 `python api_server.py` 入口启动，日志确认一个 gthread HTTP worker 在监听后自动登记成功。普通用户发起的八节点正式工作流通过公共 WorkflowRunner 执行，CSV、JSON、Parquet 各 20 行、金额合计 2100；实际 Standalone Worker 完成 37 个任务，Runtime 连续八次状态查询一致。Console 重扫、四文件预览和同一执行详情通过，应用、容器及 Infra 清理均通过且零残留。[Platform CI](https://github.com/pampa0629/addp/actions/runs/37254827587) 与 [Release/T2](https://github.com/pampa0629/addp/actions/runs/37254827477) 均全部通过。执行快照仍仅在 Runtime 进程内保存，不提供跨重启恢复。

```bash
# 在仓库根目录运行标准入口
make test-spark-workflow
make test-hdfs-online-runner
```

## 许可证

与 ADDP 平台保持一致

## 相关文档

- [Apache Spark 文档](https://spark.apache.org/docs/latest/)
- [Apache Sedona 文档](https://sedona.apache.org/)
- [ADDP 开发指南](../../docs/spec/addp开发原则.md)
- [ADDP 工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md)
