# Spark 工作流 空间计算引擎

Spark 工作流 Engine 是 ADDP 平台的分布式空间计算引擎,基于 Apache Spark 和 Apache Sedona (原 GeoSpark) 构建,支持大规模空间数据处理和分析。

## 核心特性

- **分布式计算**: 支持 TB 级空间数据处理,自动并行化
- **Sedona 空间算子**: 提供 12 个核心空间算子 (buffer, intersection, spatial_join 等)
- **统一存储访问**: 支持数据库 (PostgreSQL/MySQL/Doris)、对象存储 (S3/MinIO/HDFS)、湖仓 (Iceberg/Delta/Hudi)
- **动态 Spark 资源**: 用户注册多个 Spark 集群,工作流执行时选择
- **DAG 工作流**: 拓扑排序 + DataFrame 内存传递,最小化序列化开销
- **与 GeoPandas 互补**: 快速原型用 GeoPandas,生产大规模用 Spark

## 目录结构

```
engines/spark-workflow/
├── api_server.py           # Flask API Server (端口 8098)
├── workflow_engine.py      # 工作流执行引擎 (DAG + DataFrame 传递)
├── spark_connector.py      # 动态 SparkSession 管理器
├── storage_adapters.py     # 统一存储访问适配器
├── operators.py            # 算子实现 (22 个)
├── operator_metadata.py    # 算子元数据 (供前端使用)
├── requirements.txt        # Python 依赖
├── .env.example            # 环境变量模板
├── start.sh                # 启动脚本
└── README.md               # 本文件
```

## 算子列表 (22 个)

### 1. 数据 I/O (5个)
- `load` - 数据加载 (数据库/文件/湖仓/SQL)
- `save` - 数据保存 (数据库/文件)
- `preview` - 数据预览
- `cache` - 内存缓存
- `persist` - 持久化

### 2. Sedona 空间算子 (7个)
- `st_buffer` - 缓冲区分析
- `st_centroid` - 质心计算
- `st_intersection` - 几何相交
- `st_union` - 几何合并
- `spatial_join` - 空间连接 (核心)
- `st_distance` - 距离计算
- `st_transform` - 坐标转换

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

```bash
# 复制环境变量模板
cp .env.example .env

# 编辑 .env 文件
vi .env
```

关键配置项:
- `SYSTEM_URL`: System Backend URL (默认 http://localhost:8180)
- `SPARK_MODE`: local (开发) 或 remote (生产)
- `PORT`: Flask API Server 端口 (默认 8098)

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

### 示例 1: 单算子执行

```bash
curl -X POST http://localhost:8098/api/operators/st_buffer/execute \
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
            "schema": "public",
            "table": "poi_data"
          },
          "depends_on": []
        },
        {
          "id": "buffer_analysis",
          "operator": "st_buffer",
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

工作流定义支持两种格式:

### 格式 1: 数组格式 (推荐)

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
        "operator": "st_buffer",
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

### 格式 2: Map 格式

```json
{
  "engine_id": 34,
  "workflow_def": {
    "step1": {
      "operator": "load",
      "inputs": {...}
    },
    "step2": {
      "operator": "st_buffer",
      "inputs": {
        "input_df": {"$ref": "step1"},
        "distance": 100
      },
      "depends_on": ["step1"]
    }
  }
}
```

## 核心设计

### 动态 Spark 资源管理

Spark 工作流 Engine 不内置 Spark 集群,而是动态连接到用户注册的 Spark 资源:

1. 用户在 System 模块注册 Spark 资源 (多个)
2. 创建工作流时选择使用哪个 Spark 资源 (engine_id)
3. Engine 通过 System API 获取资源配置,动态创建 SparkSession
4. 每个 engine_id 对应一个 SparkSession (缓存复用)

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
    "engine_id": 34,
    "schema": "public",
    "table": "poi_data"
})

# 加载: S3
df = StorageAdapter.load(spark, {
    "source_type": "file",
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

## 与 Python Workflow Engine 的对比

| 特性 | Python Workflow Engine | Spark 工作流 Engine |
|------|------------------|---------------------|
| **适用场景** | 快速原型、探索分析 | 生产环境、大规模处理 |
| **数据规模** | < 10 GB | > 100 GB (TB 级) |
| **并行化** | 单机多进程 | 分布式集群 |
| **空间算子** | 21 个 (Shapely/GeoPandas) | 22 个 (Sedona) |
| **性能** | 单机 CPU/内存限制 | 集群自动扩展 |
| **部署方式** | 内置服务 (端口 8099) | 动态注册 (端口 8098) |
| **存储支持** | 数据库 + S3 | 数据库 + S3 + 湖仓 (Iceberg/Delta) |

**使用建议**:
- **原型开发**: 使用 GeoPandas (快速迭代)
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

### 执行单个算子
```
POST /api/operators/{operator_name}/execute
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

1. 在 `operators.py` 中实现算子函数:
```python
def my_operator(input_df: DataFrame, param1: str, param2: int) -> DataFrame:
    """算子说明"""
    # 实现逻辑
    return result_df
```

2. 注册到 `_OPERATOR_REGISTRY`:
```python
_OPERATOR_REGISTRY["my_operator"] = my_operator
```

3. 在 `operator_metadata.py` 中添加元数据:
```python
{
    "id": "my_operator",
    "name": "my_operator",
    "display_name": "我的算子",
    "module": "spark",
    "category": "自定义",
    "parameters": [...],
    "output_ports": [...]
}
```

### 运行测试

```bash
# 测试工作流引擎
python3 workflow_engine.py

# 测试算子
python3 -c "from operators import get_operator; op = get_operator('st_buffer'); print(op)"
```

## 许可证

与 ADDP 平台保持一致

## 相关文档

- [Apache Spark 文档](https://spark.apache.org/docs/latest/)
- [Apache Sedona 文档](https://sedona.apache.org/)
- [ADDP 开发指南](../../docs/spec/addp开发原则.md)
- [Python Workflow Engine 文档](../python-workflow/docs/python-workflow-gis引擎.md)
