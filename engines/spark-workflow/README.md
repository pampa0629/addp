# Spark 工作流 空间计算引擎

Spark 工作流 Engine 是 ADDP 平台的分布式空间计算引擎,基于 Apache Spark 和 Apache Sedona (原 GeoSpark) 构建,支持大规模空间数据处理和分析。

## 核心特性

- **分布式计算**: 支持 TB 级空间数据处理,自动并行化
- **Sedona 空间算子**: 提供核心空间算子 (buffer, intersection, spatial_join 等)
- **统一存储访问**: 支持数据库 (PostgreSQL/MySQL/Doris)、对象存储 (S3/MinIO/HDFS)、湖仓 (Iceberg/Delta/Hudi)
- **动态 Spark 资源**: 用户注册多个 Spark 集群,工作流执行时选择
- **DAG 工作流**: 拓扑排序 + DataFrame 内存传递,最小化序列化开销
- **与 GeoPython Workflow 互补**: 快速原型用 GeoPython Workflow,生产大规模用 Spark

## 目录结构

```
engines/spark-workflow/
├── api_server.py           # Flask API Server (端口 8098)
├── workflow_engine.py      # 工作流执行引擎 (DAG + DataFrame 传递)
├── spark_connector.py      # 动态 SparkSession 管理器
├── storage_adapters.py     # 统一存储访问适配器
├── operators/              # 算子实现与 Pydantic 元数据定义
├── operator_metadata.py    # 公开算子元数据出口 (供前端使用)
├── requirements.txt        # Python 依赖
├── start.sh                # 启动脚本
└── README.md               # 本文件
```

## 算子列表

### 1. 数据 I/O (5个)
- `load` - 数据加载 (数据库/文件/湖仓/SQL)
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

本地开发运行时固定使用 OpenJDK 11，与 Business Spark Master/Worker 保持一致。Spark 3.5 / Hadoop 3.3 不支持直接运行在 JDK 25 上，且分布式 driver/executor 不得混用 JVM 主版本；macOS 可通过 `brew install openjdk@11` 安装，`scripts/dev/start.sh` 和 `scripts/dev/restart.sh` 会自动选择该版本。

当注册的 Spark Master 使用 `localhost` 时，Workflow driver 会绑定本机所有地址，并通过 `SPARK_WORKFLOW_SHARED_HOST` 公布一个 driver 和 executor 都可访问的宿主机地址。本地启动脚本默认探测当前默认网络接口的 IPv4 地址，也可由部署者显式配置。

Spark JDBC 的 schema 解析发生在 driver，分区读取和写入发生在 executor，因此两端必须使用同一个可达地址。当数据引擎连接地址是 loopback 时，Spark Workflow 只在构造 JDBC URL 时使用 `SPARK_WORKFLOW_SHARED_HOST`，System 中保存的连接配置不变；远程主机地址不会被改写。PostgreSQL JDBC URL 同时继承 System `connection_info.sslmode`。

保存到 PostgreSQL 时，Runtime 按 DataFrame schema 识别 Sedona Geometry 列，不依赖固定列名。空间结果先以 EWKT 写入同 schema 的唯一暂存表，再在 PostgreSQL 事务中转换为 PostGIS `geometry` 并替换或追加目标表；暂存表不作为工作流产物暴露。

### 1. 安装依赖

```bash
# 创建虚拟环境 (推荐)
cd engines/spark-workflow
python3 -m venv venv
source venv/bin/activate

# 安装依赖
pip install -r requirements.txt
```

### 2. 配置环境变量

开发环境统一使用 ADDP 仓库根目录 `.env`；`start.sh` 会自动加载该文件，不再创建或读取引擎目录内的 `.env`。

关键配置项:
- `SYSTEM_URL`: System Backend URL (默认 http://localhost:8180)
- `SPARK_MODE`: local (开发) 或 remote (生产)
- `SPARK_WORKFLOW_PORT`: Flask API Server 端口 (默认 8098)

### 3. 启动服务

```bash
# 使用启动脚本
./start.sh

# 或手动启动
python3 api_server.py
```

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

`SparkWorkflowEngine` 执行流程:

1. 加载工作流定义 → 构建任务图
2. 拓扑排序 → 确定执行顺序
3. 逐步执行 → DataFrame 内存传递 (避免序列化)
4. 引用解析 → `{"$ref": "task_id"}` 获取上游结果

```python
engine = SparkWorkflowEngine(engine_id=34)
engine.load_workflow(workflow_def)
result = engine.run()
```

## 与 GeoPython Workflow Engine 的对比

| 特性 | GeoPython Workflow Engine | Spark 工作流 Engine |
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
    .config("spark.jars.packages", "org.apache.sedona:sedona-spark-shaded-3.5_2.12:1.5.1,org.datasyslab:geotools-wrapper:1.5.1-28.2,org.postgresql:postgresql:42.7.4,com.mysql:mysql-connector-j:8.4.0") \
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

```bash
# 测试工作流引擎
python3 workflow_engine.py

# 测试算子
python3 -c "from operators import get_operator; op = get_operator('buffer'); print(op)"
```

## 许可证

与 ADDP 平台保持一致

## 相关文档

- [Apache Spark 文档](https://spark.apache.org/docs/latest/)
- [Apache Sedona 文档](https://sedona.apache.org/)
- [ADDP 开发指南](../../docs/spec/addp开发原则.md)
- [ADDP 工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md)
