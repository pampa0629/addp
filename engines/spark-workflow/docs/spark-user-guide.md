# Spark 通用引擎与 Sedona 用户手册

本文档介绍如何在 ADDP 平台中注册 `spark` 通用引擎资源，并使用 Apache Spark 和 Sedona 进行大规模数据分析和空间计算。若通过 Spark Workflow 执行工作流，还必须在执行请求中把顶层 `engine_id` 绑定到这里注册的 `spark` 通用引擎资源。

## 目录

- [1. 概述](#1-概述)
- [2. 部署和启动](#2-部署和启动)
- [3. 注册 Apache Spark 通用引擎资源](#3-注册-apache-spark-通用引擎资源)
- [4. 使用 SQL 工作台](#4-使用-sql-工作台)
- [5. 数据加载方式](#5-数据加载方式)
- [6. Sedona 空间函数参考](#6-sedona-空间函数参考)
- [7. 常见 SQL 示例](#7-常见-sql-示例)
- [8. 性能优化建议](#8-性能优化建议)
- [9. 故障排查](#9-故障排查)

---

## 1. 概述

### 1.1 什么是 Apache Spark？

Apache Spark 是分布式计算引擎，可通过 Spark SQL / Thrift Server 处理结构化数据。它提供：

- **分布式 SQL 查询引擎**：支持标准 SQL 语法
- **多数据源支持**：可访问 PostgreSQL、MySQL、S3/MinIO、CSV、Parquet 等
- **高性能**：内存计算 + 分布式并行处理

### 1.2 什么是 Apache Sedona？

Apache Sedona（原 GeoSpark）是 Spark 的空间数据处理扩展，提供：

- **100+ 空间函数**：覆盖几何创建、转换、关系判断、度量计算等
- **空间索引**：R-Tree、Quad-Tree 加速空间查询
- **空间连接**：高效的空间关联查询（如 point-in-polygon）
- **GeoParquet 支持**：高效的列式空间数据存储格式

### 1.3 ADDP 平台集成架构

```
┌─────────────────────────────────────────────────────────────┐
│  ADDP Develop Module                                         │
│  - SQL 工作台选择 spark 通用引擎                              │
│  - Spark Workflow 执行时绑定 spark engine_id                  │
│  - 编写 SQL 或执行算子 DAG                                   │
└─────────────────┬───────────────────────────────────────────┘
                  │ JDBC / Spark general engine binding
                  ▼
┌─────────────────────────────────────────────────────────────┐
│  Spark Thrift Server (业务网络)                             │
│  - 接收 SQL 请求                                            │
│  - 调用 Apache Spark 引擎                                      │
│  - 加载 Sedona 扩展                                         │
└─────────────────┬───────────────────────────────────────────┘
                  │
      ┌───────────┼───────────┬───────────────┐
      ▼           ▼           ▼               ▼
   [PostgreSQL] [MySQL]  [MinIO S3]    [Spark Worker]
   (JDBC 外部表)(业务数据) (GeoParquet)  (分布式计算)
```

---

## 2. 部署和启动

### 2.1 启动 Spark 集群

Spark 集群部署在 `business` 业务网络中，与业务数据库（PostgreSQL、MinIO）在同一网络。

```bash
# 从项目根目录
cd business

# 启动 Spark Master + Thrift Server + Worker
docker-compose up -d spark-master spark-worker-1

# （可选）启动第二个 Worker 实现高可用
docker-compose --profile full up -d spark-worker-2

# 查看服务状态
docker-compose ps
```

### 2.2 验证部署

**检查 Spark Master Web UI**:
- URL: http://localhost:18088
- 应看到 1-2 个 Active Workers

**检查 Thrift Server 端口**:
```bash
nc -zv localhost 11000
# 成功时输出 succeeded
```

**查看日志**:
```bash
docker-compose logs -f spark-master
```

### 2.3 资源配置

默认配置（单 Worker）:
- Master: 4GB 内存, 2 核 CPU
- Worker: 2GB 内存, 2 核 CPU
- 总计: **6GB 内存, 4 核 CPU**

如需调整，修改 `business/spark/conf/spark-defaults.conf`:
```properties
spark.executor.memory=4g    # Worker 内存
spark.driver.memory=2g      # Master 内存
spark.default.parallelism=8 # 并行度
```

---

## 3. 注册 Apache Spark 通用引擎资源

在 ADDP System 模块中注册 Apache Spark 作为通用引擎资源。该资源的 `engine_type` 必须为 `spark`，表达真实 Spark Thrift Server 或 Spark 集群连接配置。

### 3.1 通过 System 前端注册

1. 登录 ADDP Console → System 模块 → 资源管理
2. 点击"新建资源"
3. 填写信息：
   - **资源名称**: `Apache Spark 分析引擎`
   - **资源类型**: `spark`
   - **连接配置**:
     ```json
     {
       "host": "host.docker.internal",
       "port": 11000,
       "master_port": 7077,
       "database": "default"
     }
     ```
   - **描述**: Apache Spark 分布式查询引擎，支持 Sedona 空间函数
4. 点击"测试连接"验证
5. 保存

### 3.2 通过 API 注册

```bash
TOKEN="<your_jwt_token>"

curl -X POST http://localhost:8180/api/v1/system/engines \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Apache Spark 分析引擎",
    "engine_type": "spark",
    "connection_info": {
      "host": "host.docker.internal",
      "port": 11000,
      "master_port": 7077,
      "database": "default"
    },
    "description": "Apache Spark 分布式查询引擎，支持 Sedona 空间函数"
  }'
```

### 3.3 连接配置说明

| 字段 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `host` | 是 | - | Thrift Server 地址（开发环境: `host.docker.internal`） |
| `port` | 否 | `10000` | Thrift Server 端口；Business 样例映射到宿主机 `11000` |
| `master_port` | 否 | `7077` | Spark Workflow 提交作业使用的 Standalone Master 端口 |
| `database` | 否 | `default` | 默认数据库（namespace） |
| `username` | 否 | - | 认证用户名（如启用认证） |
| `password` | 否 | - | 认证密码（如启用认证） |

### 3.4 Spark Workflow 运行时绑定

`spark_workflow` 是工作流运行时扩展引擎，默认端口为 `8098`，它本身不代表某个 Spark 集群。执行 Spark Workflow 时，标准请求体顶层 `engine_id` 必须指向本节注册的 `spark` 通用引擎实例。表、NFS 文件和对象存储数据源在用户/Develop 侧使用 `locator` 或 `target_parent_locator + target_name` 选择，Develop 后端在调用运行时时派生为 `connection_info`、`schema/table` 或 `path`：

```json
{
  "engine_id": 34,
  "workflow_def": {
    "tasks": [
      {
        "id": "load_table",
        "operator": "load",
        "params": {
          "source_type": "table",
          "locator": "addp://engine/12/path/public/china_pois?type=table&item_id=99"
        },
        "depends_on": []
      }
    ]
  },
  "input_data": {}
}
```

Develop 前端的 Spark 通用引擎资源选择可使用 `spark_cluster_id` 表达用户选择，Develop 后端在调用 `WorkflowRuntimeProvider.ExecuteWorkflow()` 前必须校验该 ID 指向已启用的 `engine_type=spark` 通用引擎资源，并映射为标准请求顶层 `engine_id`。这个字段是执行期运行时资源绑定，不进入 `spark_workflow` 的 capabilities。

---

## 4. 使用 SQL 工作台

### 4.1 访问 Develop 模块

1. 登录 ADDP Console → Develop 模块
2. 进入"SQL 工作台"页面
3. 在数据源下拉框中选择 `Apache Spark 分析引擎`

### 4.2 执行 SQL 查询

**基础查询**:
```sql
-- 测试连接
SELECT 1 AS test;

-- 查看可用数据库
SHOW DATABASES;

-- 查看表
SHOW TABLES IN default;

-- 简单查询
SELECT * FROM my_table LIMIT 10;
```

**带空间函数的查询**:
```sql
-- 创建空间数据
SELECT
  ST_Point(116.4074, 39.9042) AS beijing_point,
  ST_GeomFromText('POLYGON((0 0, 10 0, 10 10, 0 10, 0 0))') AS square;

-- 空间计算
SELECT
  ST_Distance(
    ST_Point(116.4074, 39.9042),  -- 北京
    ST_Point(121.4737, 31.2304)   -- 上海
  ) AS distance_degrees;
```

### 4.3 查询结果

- **列信息**: 自动识别列名和数据类型
- **结果集**: 以表格形式展示，支持翻页（默认最多 1000 行）
- **执行时间**: 显示查询耗时（毫秒）
- **错误信息**: 如有错误，显示详细错误堆栈

---

## 5. 数据加载方式

### 5.1 方法 A: JDBC 外部表（实时小规模）

适用场景: 实时查询、数据量 < 100MB、低延迟要求

**PostgreSQL 外部表**:
```sql
-- 创建外部表
CREATE TABLE postgres_users
USING jdbc
OPTIONS (
  url "jdbc:postgresql://host.docker.internal:5433/business",
  dbtable "public.users",
  user "business",
  password "business_password",
  driver "org.postgresql.Driver"
);

-- 查询外部表
SELECT * FROM postgres_users WHERE age > 30;
```

**MySQL 外部表**:
```sql
CREATE TABLE mysql_orders
USING jdbc
OPTIONS (
  url "jdbc:mysql://host.docker.internal:3306/business",
  dbtable "orders",
  user "root",
  password "password",
  driver "com.mysql.cj.jdbc.Driver"
);

SELECT COUNT(*) FROM mysql_orders WHERE order_date > '2024-01-01';
```

**优点**: 实时数据、无需预处理
**缺点**: 查询性能受限于源数据库、不适合大表

### 5.2 方法 B: GeoParquet 文件（批量大规模）

适用场景: 数据量 > 1GB、批量分析、空间数据

**加载 GeoParquet**:
```sql
-- 从 MinIO 加载 GeoParquet
CREATE TABLE spatial_data
USING parquet
OPTIONS (
  path "s3a://business-data/geoparquet/beijing_buildings.parquet"
);

-- 空间查询
SELECT
  building_name,
  ST_Area(geom) AS area_sqm
FROM spatial_data
WHERE ST_Intersects(
  geom,
  ST_MakeEnvelope(116.3, 39.8, 116.5, 40.0)  -- 北京某区域
);
```

**S3/MinIO 配置** (已在 `spark-defaults.conf` 中预配置):
```properties
spark.hadoop.fs.s3a.endpoint=http://business-minio:9000
spark.hadoop.fs.s3a.access.key=minioadmin
spark.hadoop.fs.s3a.secret.key=minioadmin
spark.hadoop.fs.s3a.path.style.access=true
```

**通过 Transfer 模块导出 GeoParquet**:
- 选择 PostgreSQL/PostGIS 表
- 选择导出格式: GeoParquet
- 目标存储: MinIO (`s3a://business-data/geoparquet/`)
- 触发导出执行

### 5.3 数据加载性能对比

| 方法 | 数据量 | 首次查询延迟 | 查询性能 | 推荐场景 |
|------|--------|--------------|----------|----------|
| JDBC 外部表 | < 100MB | 低 (< 1s) | 中等 | 实时查询、小表关联 |
| GeoParquet | > 1GB | 中等 (5-30s) | 高 | 批量分析、空间聚合 |

---

## 6. Sedona 空间函数参考

Sedona 提供 100+ 空间函数，以下是常用函数分类。

### 6.1 几何创建

| 函数 | 说明 | 示例 |
|------|------|------|
| `ST_Point(x, y)` | 创建点 | `ST_Point(116.4, 39.9)` |
| `ST_GeomFromText(wkt)` | 从 WKT 创建几何 | `ST_GeomFromText('POINT(0 0)')` |
| `ST_GeomFromWKB(wkb)` | 从 WKB 创建几何 | `ST_GeomFromWKB(binary_col)` |
| `ST_MakeEnvelope(xmin, ymin, xmax, ymax)` | 创建矩形 | `ST_MakeEnvelope(0, 0, 10, 10)` |
| `ST_MakePolygon(linestring)` | 创建多边形 | `ST_MakePolygon(ST_GeomFromText('LINESTRING(...)'))` |

### 6.2 空间关系判断

| 函数 | 说明 | 返回值 |
|------|------|--------|
| `ST_Contains(geom1, geom2)` | geom1 是否包含 geom2 | Boolean |
| `ST_Intersects(geom1, geom2)` | 是否相交 | Boolean |
| `ST_Within(geom1, geom2)` | geom1 是否在 geom2 内 | Boolean |
| `ST_Touches(geom1, geom2)` | 是否接触（边界相交） | Boolean |
| `ST_Overlaps(geom1, geom2)` | 是否重叠（部分相交） | Boolean |
| `ST_Crosses(geom1, geom2)` | 是否交叉 | Boolean |
| `ST_Disjoint(geom1, geom2)` | 是否不相交 | Boolean |

### 6.3 空间度量

| 函数 | 说明 | 单位 |
|------|------|------|
| `ST_Distance(geom1, geom2)` | 最短距离 | 度数（未投影）或米（已投影） |
| `ST_Area(geom)` | 面积 | 平方单位 |
| `ST_Length(geom)` | 长度/周长 | 长度单位 |
| `ST_Perimeter(geom)` | 周长（仅多边形） | 长度单位 |

### 6.4 几何处理

| 函数 | 说明 | 返回值 |
|------|------|--------|
| `ST_Buffer(geom, distance)` | 缓冲区 | Geometry |
| `ST_Centroid(geom)` | 质心 | Point |
| `ST_ConvexHull(geom)` | 凸包 | Geometry |
| `ST_Intersection(geom1, geom2)` | 交集 | Geometry |
| `ST_Union(geom1, geom2)` | 并集 | Geometry |
| `ST_Difference(geom1, geom2)` | 差集 | Geometry |
| `ST_Simplify(geom, tolerance)` | 简化 | Geometry |

### 6.5 坐标转换

| 函数 | 说明 |
|------|------|
| `ST_Transform(geom, source_srid, target_srid)` | 坐标系转换 |
| `ST_SetSRID(geom, srid)` | 设置空间参考 |
| `ST_SRID(geom)` | 获取 SRID |

### 6.6 几何属性

| 函数 | 说明 | 返回值 |
|------|------|--------|
| `ST_GeometryType(geom)` | 几何类型 | String |
| `ST_IsValid(geom)` | 是否有效 | Boolean |
| `ST_IsEmpty(geom)` | 是否为空 | Boolean |
| `ST_NumGeometries(geom)` | 子几何数量 | Integer |
| `ST_X(point)` | X 坐标 | Double |
| `ST_Y(point)` | Y 坐标 | Double |

---

## 7. 常见 SQL 示例

### 7.1 空间范围查询（Bounding Box）

**场景**: 查询北京市中心区域内的 POI

```sql
SELECT
  poi_id,
  poi_name,
  ST_X(geom) AS longitude,
  ST_Y(geom) AS latitude
FROM poi_table
WHERE ST_Intersects(
  geom,
  ST_MakeEnvelope(116.3, 39.8, 116.5, 40.0)  -- 北京中心区域
);
```

### 7.2 缓冲区分析（Buffer Analysis）

**场景**: 查找地铁站 1km 范围内的商场

```sql
WITH subway_buffers AS (
  SELECT
    station_id,
    station_name,
    ST_Buffer(
      ST_Transform(geom, 4326, 3857),  -- 转换到投影坐标系
      1000  -- 1000米缓冲区
    ) AS buffer_geom
  FROM subway_stations
)
SELECT
  s.station_name,
  m.mall_name,
  ST_Distance(
    ST_Transform(s.geom, 4326, 3857),
    ST_Transform(m.geom, 4326, 3857)
  ) AS distance_meters
FROM subway_buffers sb
JOIN malls m ON ST_Intersects(sb.buffer_geom, ST_Transform(m.geom, 4326, 3857))
JOIN subway_stations s ON sb.station_id = s.station_id;
```

### 7.3 空间聚合（Spatial Aggregation）

**场景**: 按行政区统计建筑面积

```sql
SELECT
  district_name,
  COUNT(*) AS building_count,
  SUM(ST_Area(ST_Transform(b.geom, 4326, 3857))) AS total_area_sqm
FROM buildings b
JOIN districts d ON ST_Within(b.geom, d.geom)
GROUP BY district_name
ORDER BY total_area_sqm DESC;
```

### 7.4 最近邻查询（K-NN）

**场景**: 查找离指定点最近的 10 个餐馆

```sql
SELECT
  restaurant_name,
  ST_Distance(
    geom,
    ST_Point(116.4074, 39.9042)  -- 查询点
  ) AS distance_degrees
FROM restaurants
ORDER BY distance_degrees ASC
LIMIT 10;
```

### 7.5 空间连接（Spatial Join）

**场景**: 统计每个商圈内的用户签到数

```sql
SELECT
  cbd.area_name,
  COUNT(DISTINCT c.user_id) AS unique_users,
  COUNT(*) AS total_checkins
FROM checkins c
JOIN cbd_areas cbd ON ST_Within(c.geom, cbd.geom)
GROUP BY cbd.area_name
ORDER BY total_checkins DESC;
```

### 7.6 路径分析（Line Processing）

**场景**: 计算公交线路总长度

```sql
SELECT
  route_id,
  route_name,
  ST_Length(ST_Transform(geom, 4326, 3857)) / 1000 AS length_km
FROM bus_routes
ORDER BY length_km DESC;
```

### 7.7 几何简化（Simplification）

**场景**: 简化边界以减少数据量

```sql
SELECT
  region_id,
  region_name,
  ST_Simplify(geom, 0.001) AS simplified_geom  -- 容差 0.001 度
FROM region_boundaries
WHERE ST_NumPoints(geom) > 10000;  -- 仅简化复杂边界
```

---

## 8. 性能优化建议

### 8.1 数据分区

按空间或时间分区提升查询性能：

```sql
-- 创建分区表（按日期）
CREATE TABLE checkins_partitioned (
  user_id INT,
  checkin_time TIMESTAMP,
  geom GEOMETRY
) PARTITIONED BY (checkin_date DATE);

-- 查询时自动分区剪枝
SELECT COUNT(*) FROM checkins_partitioned
WHERE checkin_date BETWEEN '2024-01-01' AND '2024-01-31';
```

### 8.2 空间索引

Sedona 自动使用空间索引加速查询：

```sql
-- 确保几何列有空间索引（Sedona 自动处理）
-- 大表查询时使用空间谓词触发索引
SELECT * FROM large_table
WHERE ST_Intersects(geom, ST_MakeEnvelope(...));
```

### 8.3 投影坐标系

使用投影坐标系（如 EPSG:3857）进行度量计算：

```sql
-- ❌ 错误：WGS84 (EPSG:4326) 度数单位不准确
SELECT ST_Distance(
  ST_Point(116.4, 39.9),
  ST_Point(116.5, 40.0)
) AS distance_degrees;  -- 结果约 0.14 度（无实际意义）

-- ✅ 正确：转换到投影坐标系计算米
SELECT ST_Distance(
  ST_Transform(ST_Point(116.4, 39.9), 4326, 3857),
  ST_Transform(ST_Point(116.5, 40.0), 4326, 3857)
) AS distance_meters;  -- 结果约 14000 米
```

### 8.4 缓存中间结果

```sql
-- 缓存常用的中间表
CACHE TABLE frequent_queries AS
SELECT * FROM large_table WHERE important_condition;

-- 后续查询使用缓存
SELECT COUNT(*) FROM frequent_queries;

-- 清理缓存
UNCACHE TABLE frequent_queries;
```

### 8.5 并行度调整

修改 `spark-defaults.conf`:
```properties
spark.default.parallelism=16        # 任务并行度
spark.sql.shuffle.partitions=16     # Shuffle 分区数
```

或在 SQL 中动态设置：
```sql
SET spark.sql.shuffle.partitions=32;
```

---

## 9. 故障排查

### 9.1 连接失败

**症状**: "Connection refused" 或 "Connection timeout"

**排查步骤**:
1. 检查 Spark Thrift Server 是否运行:
   ```bash
   docker ps | grep spark-master
   nc -zv localhost 11000
   ```

2. 查看日志:
   ```bash
   docker-compose logs -f spark-master | grep "ThriftServer"
   ```

3. 验证网络连通性:
   ```bash
   # 从 Develop 容器内测试
   docker exec -it <develop-container> nc -zv host.docker.internal 11000
   ```

### 9.2 查询超时

**症状**: "Query timeout after 60 seconds"

**解决方法**:
- 增加超时设置（Develop 模块默认 60 秒）
- 优化查询（使用分区、空间索引）
- 增加 Worker 资源

### 9.3 内存溢出

**症状**: "OutOfMemoryError: Java heap space"

**解决方法**:
1. 增加 Executor 内存 (`spark-defaults.conf`):
   ```properties
   spark.executor.memory=4g
   spark.driver.memory=2g
   ```

2. 减少分区数据量:
   ```sql
   SET spark.sql.shuffle.partitions=64;  -- 增加分区数
   ```

3. 使用 `LIMIT` 限制结果集

### 9.4 Sedona 函数未找到

**症状**: "Function ST_Point not found"

**原因**: Sedona 扩展未加载

**解决方法**:
检查 `business/docker-compose.yml` 中 Spark Master 启动命令是否包含:
```yaml
--packages org.apache.sedona:sedona-spark-shaded-3.5_2.12:1.5.1,org.datasyslab:geotools-wrapper:1.5.1-28.2
--conf spark.sql.extensions=org.apache.sedona.sql.SedonaSqlExtensions
```

### 9.5 JDBC 外部表连接失败

**症状**: "JDBC connection failed"

**排查步骤**:
1. 验证数据库可访问:
   ```bash
   docker exec -it business-spark-master bash
   nc -zv host.docker.internal 5433
   ```

2. 检查 JDBC URL 格式:
   ```sql
   -- PostgreSQL
   url "jdbc:postgresql://host.docker.internal:5433/business"

   -- MySQL
   url "jdbc:mysql://host.docker.internal:3306/business"
   ```

3. 验证驱动类名:
   - PostgreSQL: `org.postgresql.Driver`
   - MySQL: `com.mysql.cj.jdbc.Driver`

---

## 附录 A: Sedona 函数完整列表

完整函数文档: https://sedona.apache.org/latest-snapshot/api/sql/Function/

### 几何构造函数 (20+)
- ST_Point, ST_MakePoint, ST_PointZ, ST_PointM
- ST_GeomFromText, ST_GeomFromWKT, ST_GeomFromWKB, ST_GeomFromGeoJSON
- ST_LineFromText, ST_LineStringFromText, ST_MakeLine
- ST_PolygonFromText, ST_MakePolygon, ST_MakeEnvelope
- ST_MultiPoint, ST_MultiLineString, ST_MultiPolygon, ST_GeomCollFromText

### 几何访问函数 (15+)
- ST_GeometryType, ST_Dimension, ST_IsEmpty, ST_IsValid, ST_IsSimple
- ST_X, ST_Y, ST_Z, ST_M, ST_CoordDim, ST_NPoints, ST_NumGeometries
- ST_StartPoint, ST_EndPoint, ST_PointN, ST_ExteriorRing

### 空间关系函数 (12+)
- ST_Contains, ST_Within, ST_Intersects, ST_Crosses, ST_Touches
- ST_Overlaps, ST_Disjoint, ST_Equals, ST_Covers, ST_CoveredBy
- ST_Relate, ST_RelateMatch

### 空间度量函数 (8+)
- ST_Distance, ST_Distance3D, ST_HausdorffDistance, ST_FrechetDistance
- ST_Area, ST_Length, ST_Length3D, ST_Perimeter

### 几何处理函数 (30+)
- ST_Buffer, ST_ConvexHull, ST_Envelope, ST_Centroid, ST_PointOnSurface
- ST_Intersection, ST_Union, ST_Difference, ST_SymDifference
- ST_Simplify, ST_SimplifyPreserveTopology, ST_ReducePrecision
- ST_Snap, ST_Split, ST_SubDivide, ST_MakeValid
- ST_Boundary, ST_LineMerge, ST_LineSubstring, ST_ClosestPoint

### 坐标转换函数 (5+)
- ST_Transform, ST_SetSRID, ST_SRID, ST_FlipCoordinates, ST_Translate

### 聚合函数 (5+)
- ST_Union_Aggr, ST_Envelope_Aggr, ST_Intersection_Aggr, ST_Collect

---

## 附录 B: 性能基准测试

基于 ADDP 平台测试环境（6GB 内存，4 核 CPU）:

| 操作 | 数据量 | 执行时间 | 说明 |
|------|--------|----------|------|
| 创建 1000 个随机点 | 1,000 行 | < 1 秒 | 内存操作 |
| 查询 PostgreSQL 外部表 | 10 万行 | 2-5 秒 | JDBC 读取 + 序列化 |
| 加载 GeoParquet 首次 | 100 万行 | 10-20 秒 | S3 读取 + 列式解析 |
| 加载 GeoParquet 缓存后 | 100 万行 | 1-2 秒 | 内存缓存 |
| 空间范围查询 (含索引) | 100 万行 | 3-8 秒 | R-Tree 索引加速 |
| 缓冲区计算 (Buffer 1km) | 1 万行 | 5-10 秒 | 复杂几何运算 |
| 空间连接 (Point-in-Polygon) | 10 万 × 1000 | 30-60 秒 | Broadcast Join |

*注: 实际性能取决于数据复杂度、网络延迟、集群配置等因素。*

---

## 附录 C: 相关资源

- **Apache Spark 官方文档**: https://spark.apache.org/docs/latest/
- **Apache Sedona 官方文档**: https://sedona.apache.org/
- **Sedona SQL API**: https://sedona.apache.org/latest-snapshot/api/sql/Overview/
- **GeoParquet 规范**: https://geoparquet.org/
- **PostGIS 函数对照**: https://sedona.apache.org/latest-snapshot/tutorial/sql/#postgis-style-api

---

**文档版本**: v1.0
**更新日期**: 2025-12-14
**适用版本**: ADDP v0.0.14+, Spark 3.5.0, Sedona 1.5.1
