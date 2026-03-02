# Spark + Sedona 集成 ADDP 架构设计

## 设计日期
2025-12-14

## 核心设计原则

### 1. Spark 作为 SQL 数据源注册（非计算引擎）

**关键洞察**：
- Apache Spark Thrift Server 提供 JDBC 接口（端口 10000）
- 与 Doris 的使用方式完全一致
- 无需区分"是否存储数据"，关键是"是否提供 SQL 接口"

### 2. Sedona 作为 Apache Spark 的空间函数扩展

**类比 PostGIS**：
```sql
-- PostGIS 在 PostgreSQL 中
SELECT ST_Buffer(geom, 1000) FROM poi;

-- Sedona 在 Apache Spark 中（使用方式完全相同）
SELECT ST_Buffer(geom, 1000) FROM poi;
```

**核心结论**：
- ✅ Sedona 无需独立封装
- ✅ 通过 SQL 接口使用（类似 PostGIS）
- ✅ 无需开发 Spark 工作流引擎

### 3. 无需拖拽算子界面

**Python Workflow Engine vs Spark + Sedona**：

| 特性 | Python Workflow Engine | Apache Spark + Sedona |
|------|-----------------|-------------------|
| **目标用户** | GIS 专家（非程序员） | 数据工程师（SQL 熟练） |
| **交互方式** | 拖拽算子 | SQL 编辑器 |
| **算子数量** | 21 个预定义 | 100+ SQL 函数 |
| **扩展性** | 低（需开发新算子） | 高（SQL 任意组合） |
| **是否需要拖拽** | ✅ 是（核心价值） | ❌ 否（SQL 更灵活） |

---

## 架构设计

### 部署架构

```yaml
business/docker-compose.yml:
  services:
    # Spark 集群
    spark-master:
      image: bitnami/spark:3.5.0
      container_name: spark-master
      environment:
        - SPARK_MODE=master
        - SPARK_MASTER_HOST=spark-master
        - SPARK_MASTER_PORT=7077
      ports:
        - "7077:7077"   # Spark 集群通信
        - "8080:8080"   # Spark Master Web UI
        - "10000:10000" # Thrift Server (SQL 接口)
      command: >
        bash -c "
          /opt/bitnami/spark/sbin/start-master.sh &&
          /opt/bitnami/spark/sbin/start-thriftserver.sh
            --master spark://spark-master:7077
            --packages org.apache.sedona:sedona-spark-3.5_2.12:1.5.1,org.datasyslab:geotools-wrapper:1.5.1-28.2
            --conf spark.sql.extensions=org.apache.sedona.sql.SedonaSqlExtensions
            --conf spark.serializer=org.apache.spark.serializer.KryoSerializer
            --conf spark.kryo.registrator=org.apache.sedona.core.serde.SedonaKryoRegistrator
        "
      networks:
        - business-network
      restart: unless-stopped

    spark-worker-1:
      image: bitnami/spark:3.5.0
      container_name: spark-worker-1
      environment:
        - SPARK_MODE=worker
        - SPARK_MASTER_URL=spark://spark-master:7077
        - SPARK_WORKER_MEMORY=4G
        - SPARK_WORKER_CORES=2
      depends_on:
        - spark-master
      networks:
        - business-network
      restart: unless-stopped

    spark-worker-2:
      image: bitnami/spark:3.5.0
      container_name: spark-worker-2
      environment:
        - SPARK_MODE=worker
        - SPARK_MASTER_URL=spark://spark-master:7077
        - SPARK_WORKER_MEMORY=4G
        - SPARK_WORKER_CORES=2
      depends_on:
        - spark-master
      networks:
        - business-network
      restart: unless-stopped
```

### 注册到 ADDP System

**引擎类型：database（而非 compute_engine）**

```json
{
  "resource_type": "database",
  "subtype": "spark",
  "name": "Apache Spark Engine with Sedona",
  "description": "分布式 SQL 查询引擎，支持 100+ 空间分析函数（Sedona）",
  "connection_info": {
    "host": "spark-master",
    "port": 10000,
    "protocol": "hive2",
    "driver_class": "org.apache.hive.jdbc.HiveDriver",
    "jdbc_url": "jdbc:hive2://spark-master:10000/default",
    "properties": {
      "auth": "NONE"
    }
  }
}
```

**注册位置**：
- 在 ADDP System 模块的"引擎管理"中手动注册
- resource_type: `database`（与 PostgreSQL、Doris 相同）
- 存储在 `system.engines` 表中

---

## 使用方式

### 1. 在 Develop 模块中使用

#### 步骤 1：选择数据源
在 Develop 模块的 SQL 编辑器中，选择数据源：
- **数据源名称**：Apache Spark Engine with Sedona
- **连接类型**：Apache Spark (Hive2)

#### 步骤 2：输入 SQL（含 Sedona 空间函数）
```sql
-- 示例 1：POI 点缓冲区分析
SELECT
    id,
    name,
    ST_Buffer(ST_Point(lon, lat), 1000) as buffer_geom,
    ST_Area(ST_Buffer(ST_Point(lon, lat), 1000)) as buffer_area
FROM poi
WHERE city = '北京'
LIMIT 100;

-- 示例 2：空间关联（查找地铁站 500 米内的 POI）
SELECT
    p.id,
    p.name,
    s.station_name,
    ST_Distance(ST_Point(p.lon, p.lat), ST_Point(s.lon, s.lat)) as distance
FROM poi p
CROSS JOIN subway_stations s
WHERE ST_DWithin(
    ST_Point(p.lon, p.lat),
    ST_Point(s.lon, s.lat),
    500
)
ORDER BY distance;

-- 示例 3：空间聚合（按行政区统计 POI 数量）
SELECT
    d.district_name,
    COUNT(*) as poi_count,
    ST_Union_Agg(ST_Point(p.lon, p.lat)) as poi_cluster
FROM poi p
JOIN districts d
ON ST_Contains(d.geom, ST_Point(p.lon, p.lat))
GROUP BY d.district_name;
```

#### 步骤 3：执行并查看结果
- 点击"执行"按钮
- 结果以表格形式展示
- 空间几何字段显示为 WKT 格式

### 2. 在 Orchestrator 工作流中使用

```json
{
  "name": "POI 空间分析工作流",
  "tasks": [
    {
      "id": "step_1_extract_poi",
      "engine_identifier": "develop.sql.default",
      "parameters": {
        "engine_id": "{{spark_engine_id}}",
        "query": "SELECT id, name, lon, lat FROM poi WHERE city='北京'"
      }
    },
    {
      "id": "step_2_spatial_buffer",
      "engine_identifier": "develop.sql.default",
      "parameters": {
        "engine_id": "{{spark_engine_id}}",
        "query": "SELECT id, name, ST_Buffer(ST_Point(lon, lat), 1000) as buffer_geom FROM ({{step_1_extract_poi.result}})"
      },
      "depends_on": ["step_1_extract_poi"]
    },
    {
      "id": "step_3_save_result",
      "engine_identifier": "transfer.batch_worker.default",
      "parameters": {
        "source": "spark_results",
        "target_engine_id": "{{business_postgres_id}}",
        "table_name": "poi_buffer_results"
      },
      "depends_on": ["step_2_spatial_buffer"]
    }
  ]
}
```

---

## 与现有组件的对比

### Apache Spark vs Doris vs PostgreSQL

| 特性 | PostgreSQL + PostGIS | Doris | Apache Spark + Sedona |
|------|---------------------|-------|-------------------|
| **资源类型** | database | database | database |
| **JDBC 协议** | PostgreSQL | MySQL | Hive2 |
| **端口** | 5432 | 9030 | 10000 |
| **空间函数** | ST_* (PostGIS) | - | ST_* (Sedona) |
| **分布式** | 单机 | 分布式 | 分布式 |
| **数据存储** | 自身存储 | 自身存储 | 外部存储 (HDFS/S3/Hive) |
| **使用方式** | SQL 编辑器 | SQL 编辑器 | SQL 编辑器 |
| **是否需要封装** | ❌ 否 | ❌ 否 | ❌ 否 |

### Apache Spark + Sedona vs Python Workflow Engine

| 特性 | Python Workflow Engine | Apache Spark + Sedona |
|------|-----------------|-------------------|
| **架构类型** | Python 库封装 + Flask API | SQL 引擎（原生支持） |
| **部署位置** | 独立容器（python-workflow-engine） | Spark 集群（business） |
| **交互方式** | 拖拽算子 + REST API | SQL 编辑器 |
| **算子数量** | 21 个预定义 | 100+ SQL 函数 |
| **执行规模** | 单机（内存限制） | 分布式（可扩展） |
| **是否需要封装** | ✅ 是（必须） | ❌ 否（SQL 足够） |
| **元数据存储** | develop.spatial_tasks | 无需存储（直接执行 SQL） |
| **注册方式** | compute_engine | database |

---

## Sedona 空间函数速查

### 几何构造函数

```sql
ST_Point(lon, lat) -- 创建点
ST_LineString(array_of_points) -- 创建线
ST_Polygon(array_of_linestrings) -- 创建多边形
ST_MakeEnvelope(xmin, ymin, xmax, ymax, srid) -- 创建矩形
```

### 空间关系判断

```sql
ST_Contains(geom1, geom2) -- geom1 包含 geom2
ST_Within(geom1, geom2) -- geom1 在 geom2 内
ST_Intersects(geom1, geom2) -- 相交判断
ST_Disjoint(geom1, geom2) -- 不相交判断
ST_DWithin(geom1, geom2, distance) -- 距离判断
```

### 空间处理函数

```sql
ST_Buffer(geom, distance) -- 缓冲区
ST_Centroid(geom) -- 中心点
ST_ConvexHull(geom) -- 凸包
ST_Intersection(geom1, geom2) -- 交集
ST_Union(geom1, geom2) -- 并集
ST_Difference(geom1, geom2) -- 差集
```

### 空间度量函数

```sql
ST_Distance(geom1, geom2) -- 距离
ST_Area(geom) -- 面积
ST_Length(geom) -- 长度
ST_Perimeter(geom) -- 周长
```

### 空间聚合函数

```sql
ST_Union_Agg(geom) -- 聚合并集
ST_Envelope_Aggr(geom) -- 聚合外包矩形
```

### 格式转换函数

```sql
ST_AsText(geom) -- 转 WKT
ST_AsGeoJSON(geom) -- 转 GeoJSON
ST_GeomFromWKT(wkt) -- 从 WKT 创建
ST_GeomFromGeoJSON(json) -- 从 GeoJSON 创建
```

---

## 实施步骤

### Phase 1: 部署 Spark 集群（1 天）

1. **修改 `business/docker-compose.yml`**
   - 添加 spark-master, spark-worker-1, spark-worker-2 服务
   - 配置 Thrift Server 启动（端口 10000）
   - 预加载 Sedona JAR 包

2. **启动并验证**
   ```bash
   cd business
   docker-compose up -d spark-master spark-worker-1 spark-worker-2

   # 验证 Thrift Server
   docker exec -it spark-master bash
   beeline -u jdbc:hive2://localhost:10000/default

   # 测试 Sedona 函数
   SELECT ST_Point(116.4, 39.9) as beijing;
   ```

3. **验收标准**
   - ✅ Spark Master Web UI 可访问（http://localhost:8080）
   - ✅ 2 个 Workers 成功注册
   - ✅ Thrift Server 可连接（beeline 测试通过）
   - ✅ Sedona 函数可用（ST_Point 测试通过）

### Phase 2: 在 ADDP System 中注册（0.5 天）

1. **登录 ADDP Console**
   - 访问 System 模块 → 资源管理

2. **创建新引擎**
   - 引擎类型：database
   - 子类型：spark
   - 名称：Apache Spark Engine with Sedona
   - 连接信息：
     ```json
     {
       "host": "spark-master",
       "port": 10000,
       "protocol": "hive2",
       "jdbc_url": "jdbc:hive2://spark-master:10000/default"
     }
     ```

3. **验收标准**
   - ✅ 引擎保存成功（存储在 `system.engines` 表）
   - ✅ 在引擎列表中可见

### Phase 3: 在 Develop 模块中使用（0.5 天）

1. **配置数据源连接**
   - 在 Develop 模块中添加 Apache Spark 数据源支持（如需要）
   - 确保 JDBC Driver 可用（hive-jdbc）

2. **测试 SQL 执行**
   ```sql
   -- 测试 1：基础查询
   SELECT 1 + 1 as result;

   -- 测试 2：Sedona 空间函数
   SELECT
       ST_Point(116.4, 39.9) as geom,
       ST_AsText(ST_Point(116.4, 39.9)) as wkt,
       ST_Buffer(ST_Point(116.4, 39.9), 0.01) as buffer_geom;
   ```

3. **验收标准**
   - ✅ SQL 执行成功
   - ✅ Sedona 函数正常工作
   - ✅ 结果正确展示

### Phase 4: 文档和示例（0.5 天）

1. **创建用户文档**
   - Sedona 空间函数手册（中文）
   - 常见空间分析示例 SQL
   - 性能优化建议

2. **准备示例数据集**
   - 下载测试数据（POI、行政区划、道路网络）
   - 导入到 Apache Spark 可访问的数据源
   - 编写示例查询

---

## 性能优化建议

### 1. 数据格式选择
```yaml
推荐格式: GeoParquet
原因:
  - 列式存储（查询性能高）
  - 原生空间几何支持
  - 压缩率高
  - Spark 原生支持

示例:
  df.write.format("geoparquet").save("s3://bucket/spatial_data.parquet")
```

### 2. 空间索引
```sql
-- Sedona 自动优化空间连接
SELECT /*+ BROADCAST(small_table) */ *
FROM large_table
JOIN small_table
ON ST_Contains(small_table.geom, large_table.geom);
```

### 3. 分区策略
```python
# 按空间网格分区
df.write \
  .partitionBy("grid_id") \
  .format("geoparquet") \
  .save("s3://bucket/partitioned_data")
```

### 4. 缓存热数据
```sql
CACHE TABLE poi_beijing AS
SELECT * FROM poi WHERE city = '北京';

-- 后续查询使用缓存
SELECT ST_Buffer(geom, 1000) FROM poi_beijing;
```

---

## 故障排查

### 问题 1：Thrift Server 启动失败
```bash
# 检查日志
docker logs spark-master

# 常见原因：
# 1. Sedona JAR 下载失败
# 2. 端口 10000 被占用

# 解决方案：
# 1. 预下载 JAR 包到 /opt/bitnami/spark/jars/
# 2. 修改端口配置
```

### 问题 2：Sedona 函数不可用
```sql
-- 测试 Sedona 是否加载
SHOW FUNCTIONS LIKE 'ST_%';

-- 如果为空，检查配置：
-- spark.sql.extensions=org.apache.sedona.sql.SedonaSqlExtensions
```

### 问题 3：查询性能慢
```sql
-- 检查执行计划
EXPLAIN SELECT ST_Buffer(geom, 1000) FROM poi;

-- 优化建议：
-- 1. 使用分区表
-- 2. 小表广播 (BROADCAST hint)
-- 3. 使用 GeoParquet 格式
```

---

## 未来扩展方向

### 1. Spark Submit API 支持（可选）
如果未来需要执行非 SQL 的复杂批处理任务（机器学习、图计算），可以考虑：

```yaml
扩展方式:
  - 开发 Spark Job Scheduler (Flask API)
  - 注册为 compute_engine
  - 管理任务生命周期（提交、监控、取消）

使用场景:
  - Spark MLlib 机器学习
  - GraphX 图计算
  - Spark Streaming 流处理
  - 需要调用 Python 库的复杂 ETL
```

### 2. 可视化工作流支持（可选）
如果未来用户需要类似 Python Workflow Engine 的拖拽式工作流：

```yaml
实现方式:
  - 开发"SQL 任务模板"功能
  - 用户填写参数（表名、距离、条件等）
  - 后台生成 SQL 并执行

优势:
  - 无需学习 SQL 语法
  - 复用已有 Sedona 能力
  - 维护成本低（仅模板管理）
```

---

## 总结

### 核心设计决策

| 决策点 | 选择方案 | 理由 |
|-------|---------|------|
| **Spark 定位** | database（SQL 数据源） | 提供 JDBC 接口，与 Doris 一致 |
| **Sedona 封装** | 无需封装 | SQL 函数扩展，类似 PostGIS |
| **交互方式** | SQL 编辑器 | 用户熟悉，功能更灵活 |
| **是否需要拖拽算子** | 否 | SQL 组合能力强于预定义算子 |
| **元数据存储** | 无需存储 | 直接执行 SQL，无需任务表 |

### 架构优势

1. ✅ **架构简洁**：复用 Develop 模块，无需新组件
2. ✅ **学习成本低**：与 Doris/PostgreSQL 使用方式一致
3. ✅ **功能完整**：Sedona 提供 100+ 空间函数
4. ✅ **易于维护**：无封装层，直接使用 Apache Spark
5. ✅ **扩展性强**：未来可按需添加 Spark Submit API

### 实施时间估算

- Phase 1（部署 Spark 集群）：1 天
- Phase 2（System 注册）：0.5 天
- Phase 3（Develop 测试）：0.5 天
- Phase 4（文档示例）：0.5 天

**总计**：2.5 天

---

## 附录：Sedona 完整函数列表

详见 Sedona 官方文档：https://sedona.apache.org/latest-snapshot/api/sql/Overview/

**主要函数类别**：
- 构造函数（Constructor）：20+
- 空间关系函数（Spatial Relationships）：15+
- 空间处理函数（Spatial Operators）：25+
- 空间度量函数（Measurement）：10+
- 聚合函数（Aggregation）：5+
- 格式转换函数（Format Conversion）：10+
- 空间索引函数（Spatial Indexing）：5+

总计：100+ 空间分析函数

---

## 补充：架构实施细节（2025-12-14 更新）

### 网络部署决策

**Spark 部署在 business-network（与 Doris 并列）**

| 服务 | 定位 | 协议端口 | 网络 |
|------|------|---------|------|
| PostgreSQL | 业务数据存储（OLTP） | 5433 | business-network |
| Doris | 业务数据分析（OLAP） | 9030 | business-network |
| **Apache Spark** | **业务数据计算（SQL）** | **10000** | **business-network** |

**理由**：
- 三者都是业务数据层引擎，统一管理
- 访问模式一致：ADDP 服务通过 `host.docker.internal:10000` 连接
- 资源管理一致：统一在 `business/docker-compose.yml` 中部署

### Docker 启动命令修正

**❌ 原设计问题**（容器会立即退出）：
```yaml
command: >
  bash -c "
    /opt/bitnami/spark/sbin/start-master.sh &&
    /opt/bitnami/spark/sbin/start-thriftserver.sh
  "
```

**✅ 正确方式**（前台启动 + tail 保持容器运行）：
```yaml
command: >
  bash -c "
    /opt/bitnami/spark/sbin/start-master.sh &&
    /opt/bitnami/spark/bin/spark-class org.apache.spark.sql.hive.thriftserver.HiveThriftServer2
      --master spark://spark-master:7077
      --packages org.apache.sedona:sedona-spark-3.5_2.12:1.5.1
      --conf spark.sql.extensions=org.apache.sedona.sql.SedonaSqlExtensions &&
    tail -f /dev/null
  "
```

**关键改进**：
1. 使用 `spark-class` 前台启动 Thrift Server
2. 添加 `tail -f /dev/null` 保持容器运行
3. 添加 healthcheck 检测服务可用性
4. Spark Master Web UI 端口改为 8088（避免与 System 8180 冲突）
5. 添加资源限制（避免 OOM）

### 资源配置优化

**轻量级配置（适配开发环境）**：
```yaml
spark-master:
  deploy:
    resources:
      limits:
        cpus: '2'
        memory: 4G

spark-worker-1:
  environment:
    - SPARK_WORKER_MEMORY=2G
    - SPARK_WORKER_CORES=2
  deploy:
    resources:
      limits:
        cpus: '2'
        memory: 2G

spark-worker-2:
  # 默认不启动，通过 --profile full 启用
  profiles:
    - full
```

**总资源需求**：
- 默认（单 Worker）：4GB + 2GB = 6GB 内存，4 核 CPU
- 完整（双 Worker）：4GB + 2GB + 2GB = 8GB 内存，6 核 CPU

### 数据加载方案详解

#### 方案 A：JDBC 外部表（实时数据）

**适用场景**：查询 Business PostgreSQL 中的最新数据（小规模）

```sql
-- 在 Apache Spark 中创建外部表
CREATE TABLE poi
USING org.apache.spark.sql.jdbc
OPTIONS (
  url "jdbc:postgresql://business-postgres:5432/business",
  dbtable "public.poi",
  user "business",
  password "business_password",
  driver "org.postgresql.Driver"
);

-- 空间查询（Sedona 函数）
SELECT
    id, name,
    ST_Buffer(ST_Point(lon, lat), 1000) as buffer_geom
FROM poi
WHERE city = '北京'
LIMIT 10;
```

**优势**：
- ✅ 实时数据（无需 ETL）
- ✅ 谓词下推（WHERE 条件推送到 PostgreSQL）
- ✅ 无需数据复制

**劣势**：
- ⚠️ 性能受 PostgreSQL 限制（不适合大规模计算）

#### 方案 B：GeoParquet（批量计算）

**适用场景**：大规模空间分析（千万级数据）

**步骤 1：Transfer 模块导出数据**
```go
// transfer/backend/internal/worker/postgres_to_geoparquet.go
func (w *Worker) ExecutePostgresToGeoParquet(task *PostgresToGeoParquetTask) error {
    // 1. 从 PostgreSQL 查询数据
    rows, _ := db.Query(task.Query)

    // 2. 转换为 Arrow RecordBatch（GeoParquet 格式）
    schema := arrow.NewSchema([]arrow.Field{
        {Name: "geom", Type: arrow.BinaryTypes.Binary, Metadata: arrow.NewMetadata(
            []string{"ARROW:extension:name"}, []string{"geoarrow.wkb"},
        )},
    })

    // 3. 写入 MinIO（S3 API）
    writer.Write(record)
}
```

**步骤 2：Apache Spark 读取 GeoParquet**
```sql
-- 配置 S3/MinIO 访问
SET spark.hadoop.fs.s3a.endpoint=http://business-minio:9000;
SET spark.hadoop.fs.s3a.access.key=minioadmin;
SET spark.hadoop.fs.s3a.secret.key=minioadmin;

-- 读取 GeoParquet
CREATE TABLE poi
USING parquet
OPTIONS (
  path "s3a://spark-data/poi.parquet"
);

-- 高性能空间计算
SELECT
    district,
    COUNT(*) as poi_count,
    ST_Union_Agg(ST_GeomFromWKB(geom)) as cluster
FROM poi
GROUP BY district;
```

**优势**：
- ✅ 列式存储（查询性能高 10-100 倍）
- ✅ 压缩率高（减少存储成本）
- ✅ 原生空间几何支持

### Develop 模块集成实现

**Hive JDBC 客户端选型**：`github.com/beltran/gohive`

```go
// develop/backend/internal/service/spark_executor.go
func (e *SparkSQLExecutor) Execute(ctx context.Context, engineID int, query string) (*QueryResult, error) {
    // 1. 获取 Apache Spark 引擎配置
    engine, _ := e.systemClient.GetEngine(engineID)
    host := engine.ConnectionInfo["host"].(string)
    port := int(engine.ConnectionInfo["port"].(float64))

    // 2. 创建 Hive 连接
    conn, _ := gohive.Connect(host, port, "NONE", gohive.NewConnectConfiguration())
    defer conn.Close()

    // 3. 执行查询
    cursor := conn.Cursor()
    cursor.Exec(ctx, query)

    // 4. 获取结果
    rows := cursor.FetchAll()
    return &QueryResult{Rows: rows}
}
```

### 故障排查补充

#### 问题 1：Thrift Server 不响应

**症状**：`beeline` 无法连接 `jdbc:hive2://localhost:10000`

**诊断**：
```bash
# 检查容器状态
docker ps | grep spark-master

# 检查 Thrift Server 端口
docker exec -it business-spark-master netstat -tuln | grep 10000

# 查看日志
docker logs business-spark-master | grep -i thrift
```

**常见原因**：
1. Sedona JAR 下载失败（网络问题）
2. 端口 10000 被占用
3. 启动命令错误（后台启动导致容器退出）

**解决方案**：
```bash
# 预下载 Sedona JAR 包（避免启动时下载）
docker exec -it business-spark-master bash
cd /opt/bitnami/spark/jars/
wget https://repo1.maven.org/maven2/org/apache/sedona/sedona-spark-3.5_2.12/1.5.1/sedona-spark-3.5_2.12-1.5.1.jar
```

#### 问题 2：查询超时

**症状**：大规模空间计算耗时数分钟

**解决方案**：
1. Develop 模块增加查询超时配置（默认 5 分钟）
2. 使用 GeoParquet 替代 JDBC 外部表
3. 增加 Worker 数量（`--profile full`）

#### 问题 3：Sedona 函数不可用

**诊断**：
```sql
-- 测试 Sedona 是否加载
SHOW FUNCTIONS LIKE 'ST_%';
```

**解决方案**：
检查 `spark-defaults.conf` 配置：
```properties
spark.sql.extensions=org.apache.sedona.sql.SedonaSqlExtensions
spark.serializer=org.apache.spark.serializer.KryoSerializer
spark.kryo.registrator=org.apache.sedona.core.serde.SedonaKryoRegistrator
```

### 实施时间（调整后）

| 阶段 | 工作内容 | 时间 |
|------|---------|------|
| Phase 1 | 部署 Spark 集群 | 1.5 天 |
| Phase 2 | System 注册 | 0.5 天 |
| Phase 3 | 数据加载方案 | 2 天 |
| Phase 4 | Develop 集成 | 1 天 |
| ~~Phase 5~~ | ~~Orchestrator 集成~~ | ~~暂不实施~~ |
| Phase 6 | 文档和示例 | 0.5 天 |
| **总计** | | **5.5 天** |

**不实施的内容**：
- ❌ `spark-engine/`（独立 Python Flask 服务）
- ❌ Orchestrator 拖拽式工作流集成
- ❌ `spark.sql.default` 计算引擎注册

**保留的核心能力**：
- ✅ Spark 集群部署（Thrift Server）
- ✅ System 手动注册为 `database` 资源
- ✅ Develop 模块 SQL 编辑器直接查询
- ✅ Transfer 模块数据导出到 GeoParquet
