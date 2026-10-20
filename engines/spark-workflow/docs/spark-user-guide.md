# Spark 通用引擎与 Sedona 用户手册

本文说明如何注册 `engine_type=spark` 的通用引擎并在 Develop 查询工作台使用它。Spark Workflow 的运行、存储支持与验收边界见 [Runtime README](../README.md)。

## 使用入口与当前范围

| 入口 | 执行方式 | 资源配置 |
| --- | --- | --- |
| Develop 查询工作台 | Common Spark SQL 插件通过 Hive Thrift 协议访问 Thrift Server | 选择已登记的 `spark` 引擎 |
| Develop 工作流编辑器 | `spark_workflow` Runtime 通过 Standalone Master 提交 DataFrame 作业 | 选择 Runtime，并在 `execution_config.engine_specific.spark_cluster_id` 绑定 `spark` 集群 |

两个入口共用 Spark 通用资源的连接事实，分别消费 Thrift 与 Master 端口。Workflow Runtime 的默认 HTTP 端口 8098 不能填作 Spark SQL 端口。

**Business 默认 Thrift Server 尚未启用 Sedona。** 当前 [Spark 配置](../../../business/spark/spark-defaults.conf) 中空间扩展被注释，[Compose 启动命令](../../../business/docker-compose.yml) 也未加载 Sedona 包；普通 SQL 样例可用于验证 Thrift 查询，空间 SQL 需在已加载对应 Sedona 依赖的 Thrift 环境中使用。Spark Workflow 通过自己的 [依赖入口](../spark_dependencies.py) 加载 Sedona，这不会使另一 Thrift 会话自动获得空间函数。

## 部署与注册

### 启动 Business Spark

在仓库根目录，通过标准入口启动 Business 样例集群：

```bash
bash business/scripts/start.sh -spark
```

该入口启动 Master、Thrift Server 和 Worker 1，并幂等初始化、查询验证 `default.addp_sample_orders` 五行样例视图。实际地址以启动输出和 `business/.business-state/ports.env` 为准，首选端口如下：

| 服务 | 首选宿主机端口 | 用途 |
| --- | --- | --- |
| Thrift Server | 11000（容器内 10000） | SQL 查询与目录发现 |
| Standalone Master | 7077 | Workflow 提交作业 |
| Master Web UI | 18088 | 查看 Worker 和应用状态 |

资源限额以 [Business Compose](../../../business/docker-compose.yml) 和 [Spark 配置](../../../business/spark/spark-defaults.conf) 为准。Worker 数量增加不代表 Master 或 Thrift Server 已具备高可用。Business 部署与标准生命周期见 [Business 文档](../../../business/README.md)。

### 注册 Spark 通用引擎

在 Console 的 System 引擎管理中选择“计算引擎”分组下的 Apache Spark，填写实际连接地址，测试连接后保存。登记的是 `spark` 通用资源；`spark_workflow` 从扩展运行时入口管理。

| 连接字段 | 说明 |
| --- | --- |
| `host` | 必填，调用方可达的 Spark 主机；本地环境按实际部署填写，不能把容器服务名直接当作宿主机可达地址 |
| `port` | Thrift 端口，插件默认 10000；Business 宿主机映射首选 11000 |
| `database` | 默认命名空间，默认 `default` |
| `username` / `password` | 插件连接字段；认证模式以实际插件和服务部署为准，不因填写密码而推定支持任意认证机制 |

字段及默认值由 [Spark SQL 插件](../../../common/engine/plugins/spark_sql/plugin.go) 声明。Workflow 另从同一资源的连接配置读取 `master_port`（默认 7077），它用于 Standalone 作业提交，不是 Thrift 查询端口。注册连接成功只证明该连接检查，不证明 Workflow 作业、存储端点或空间函数已可用。

### 绑定 Spark Workflow

在 Develop 工作流执行配置中选择 Spark 集群。后端校验该资源已启用且 `engine_type=spark`，再把所选 `spark_cluster_id` 映射为 Runtime 请求顶层 `engine_id`。

输入和输出使用 Public Operator Spec 的 locator 参数，由 Develop 在执行前派生内部连接与路径，并注入可信租户和执行授权；不能把含 locator 的公开定义直接当作完整 Runtime HTTP 请求。可选存储类型以 Runtime 的 `compute.workflow.storage_access` 为准，当前不声明 NFS 接入。详见 [资源绑定与执行](../README.md#资源绑定与执行)及 [Develop 资源适配规范](../../../docs/spec/workflow/Develop资源与执行.md#算子契约分层)。

## 在查询工作台执行 SQL

进入 Console → Develop → 查询工作台，选择已登记的 Spark 引擎。先运行不依赖空间扩展的查询；每条语句分别提交：

```sql
SELECT 1 AS test;
```

Business 标准启动完成后，可查询样例视图：

```sql
SELECT order_id, region, amount, ordered_on
FROM default.addp_sample_orders
ORDER BY order_id
LIMIT 10;
```

查询工作台即时执行使用统一 execution，结果为服务端受限预览；行数上限和超时取自有效执行策略，不在本手册固定为 1000 行或 60 秒。查看截断标记和执行错误后再判断结果是否完整。执行与结果边界见 [Develop 任务契约](../../../docs/spec/tasks/模块任务契约.md#develop-开发任务)。

Spark SQL 插件每次查询创建并关闭 Thrift 连接，不保证多次请求复用同一 Session。需要的命名空间写入连接配置或表的限定名；不要依赖前一次 `USE`、`SET`、临时视图或缓存命令为后续请求准备会话状态。

## 数据准备与加载边界

- SQL 查询面向所连接 Thrift Server 的 Catalog。JDBC 外部表和文件表须由集群侧预先配置好驱动、数据位置与访问权限；业务数据库或 MinIO 与 Spark 处于同一网络，并不代表连接、凭据或表已经自动配置。
- 普通 Parquet 读取不等于空间列已转换为 Geometry。使用 GeoParquet 时，须按实际 Sedona 版本和文件元数据确认读取器、几何编码与 CRS；不能只用 `USING parquet` 就假定可直接调用所有空间函数。参考 [Sedona 1.5.3 数据读取说明](https://sedona.apache.org/1.5.3/tutorial/sql/)。
- Transfer 的表格／空间格式转换由 [Transfer 模块](../../../transfer/CLAUDE.md) 负责；创建可复用业务成果与下载临时导出是不同操作，不能把下载产物路径当作已注册业务表。
- 工作流 `load` / `save` 使用已授权存储资源；具体数据库、Elasticsearch、HDFS 与 MinIO 的范围和发布保证统一见 [Runtime 存储接入说明](../README.md#存储接入与结果发布)。MinIO 保存当前仅普通 Parquet `create`，不据此声明 GeoParquet 输出。

JDBC 与文件读取的取舍应依据实际数据量、源库负载、过滤下推、网络和更新频率测量；不按固定的 100 MB／1 GB 阈值或未经测量的秒数选择。

## Sedona 空间 SQL

以下示例要求 Thrift 会话已经加载 Sedona。函数写法按 [Sedona 1.5.3 构造函数](https://sedona.apache.org/1.5.3/api/sql/Constructor/)、[处理函数](https://sedona.apache.org/1.5.3/api/sql/Function/)及[空间谓词](https://sedona.apache.org/1.5.3/api/sql/Predicate/)核对；实际部署版本不同，应查对应版本文档。

### 几何列、坐标系与单位

业务示例使用已存在且当前用户获准读取的 Catalog 表，本手册不自动创建这些表。表名和 `geom` 均为示例名称，使用时替换为实际表与几何列。WKT、WKB、GeoParquet 原始列须先按编码得到 Geometry；不要默认几何列名一定是 `geom`。

`ST_SetSRID` 标记空间参考，`ST_Transform` 才转换坐标；后者使用 `'EPSG:4326'` 等字符串 CRS 参数。经纬度示例采用 X=经度、Y=纬度。距离、缓冲和面积的单位由所用算法与坐标系决定，不能把经纬度上的平面结果直接标为米或平方米。

```sql
SELECT ST_AsText(ST_Point(116.4074, 39.9042)) AS point_wkt,
       ST_AsText(ST_Transform(ST_Point(116.4074, 39.9042),
                             'EPSG:4326', 'EPSG:32650')) AS projected_wkt;
```

下面涉及米制计算的投影示例限定北京附近、输入为 WGS84 经纬度的数据，使用 UTM 50N（EPSG:32650）。其他地区应选择适合其范围和精度要求的坐标系。

### 常用函数导航

| 用途 | 函数 |
| --- | --- |
| 构造与编码 | `ST_Point`、`ST_GeomFromText`、`ST_GeomFromWKB`、`ST_PolygonFromEnvelope`、`ST_AsText` |
| 空间关系 | `ST_Contains`、`ST_Within`、`ST_Intersects`、`ST_Touches`、`ST_Overlaps`、`ST_Crosses`、`ST_Disjoint` |
| 度量 | `ST_Distance`、`ST_DistanceSphere`、`ST_Area`、`ST_Length` |
| 几何处理 | `ST_Buffer`、`ST_Centroid`、`ST_ConvexHull`、`ST_Intersection`、`ST_Union`、`ST_Difference`、`ST_SimplifyPreserveTopology` |
| 坐标与检查 | `ST_Transform`、`ST_SetSRID`、`ST_SRID`、`ST_IsValid`、`ST_IsEmpty`、`ST_GeometryType`、`ST_NumGeometries`、`ST_X`、`ST_Y` |

### 空间范围查询

查询北京矩形区域内相交的 POI，输入点为 WGS84 经纬度：

```sql
SELECT poi_id, poi_name, ST_X(geom) AS longitude, ST_Y(geom) AS latitude
FROM poi_table
WHERE ST_Intersects(geom, ST_PolygonFromEnvelope(116.3, 39.8, 116.5, 40.0));
```

### 缓冲区分析

查找地铁站 1 km 缓冲范围内的商场，双方先转换到相同投影：

```sql
WITH stations AS (
  SELECT station_id, station_name,
         ST_Transform(geom, 'EPSG:4326', 'EPSG:32650') AS shape
  FROM subway_stations
), shops AS (
  SELECT mall_name, ST_Transform(geom, 'EPSG:4326', 'EPSG:32650') AS shape
  FROM malls
)
SELECT s.station_name, m.mall_name, ST_Distance(s.shape, m.shape) AS distance_meters
FROM stations s
JOIN shops m ON ST_Intersects(ST_Buffer(s.shape, 1000), m.shape);
```

### 空间聚合

按行政区汇总完全位于区内的建筑面积；跨界建筑未计入，边界分摊需另行定义：

```sql
SELECT d.district_name, COUNT(*) AS building_count,
       SUM(ST_Area(ST_Transform(b.geom, 'EPSG:4326', 'EPSG:32650'))) AS total_area_sqm
FROM buildings b
JOIN districts d ON ST_Within(b.geom, d.geom)
GROUP BY d.district_name
ORDER BY total_area_sqm DESC;
```

### 最近邻查询

对 WGS84 点使用球面距离排序，返回米；这是查询示例，不承诺使用专门的 K-NN 索引计划：

```sql
SELECT restaurant_name,
       ST_DistanceSphere(geom, ST_Point(116.4074, 39.9042)) AS distance_meters
FROM restaurants
ORDER BY distance_meters ASC
LIMIT 10;
```

### 空间连接

统计各商圈内签到次数与去重用户数；商圈重叠时，同一签到可能计入多个商圈：

```sql
SELECT cbd.area_name, COUNT(DISTINCT c.user_id) AS unique_users, COUNT(*) AS total_checkins
FROM checkins c
JOIN cbd_areas cbd ON ST_Within(c.geom, cbd.geom)
GROUP BY cbd.area_name
ORDER BY total_checkins DESC;
```

### 路径长度

对北京附近的 WGS84 公交线路投影后计算公里长度：

```sql
SELECT route_id, route_name,
       ST_Length(ST_Transform(geom, 'EPSG:4326', 'EPSG:32650')) / 1000 AS length_km
FROM bus_routes
ORDER BY length_km DESC;
```

### 几何简化

下面的容差为输入坐标单位；WGS84 经纬度中的 `0.001` 表示度，不表示米：

```sql
SELECT region_id, region_name,
       ST_AsText(ST_SimplifyPreserveTopology(geom, 0.001)) AS simplified_wkt
FROM region_boundaries
WHERE ST_NumPoints(geom) > 10000;
```

## 性能与故障排查

性能调整先看执行计划、实际分区、Worker 负载和数据倾斜，再决定过滤、分区与资源配置。空间谓词不保证自动获得空间索引；优化行为取决于 Sedona 版本、配置和查询计划，见 [Sedona 查询优化](https://sedona.apache.org/1.5.3/api/sql/Optimizer/)。缓存、Session 参数和集群配置由实际 Spark 会话或部署管理，不能假定查询工作台的前一次请求会影响下一次。

| 现象 | 优先核对 |
| --- | --- |
| SQL 连接拒绝或超时 | Thrift 是否就绪、实际宿主端口、调用方网络地址和服务日志；Master Web UI 可访问不等于 Thrift 可查询 |
| Workflow 无法提交或 Worker 不执行 | 所选集群与 Master 端口、Driver／Executor 双向可达性及资源额度，见 [Runtime 连接说明](../README.md#driver-与-executor-连接) |
| `ST_Point` 等函数不存在 | 当前 Thrift 会话是否加载 Sedona；Business 默认未启用，不能用 Workflow 的算子可用性证明 SQL 函数可用 |
| JDBC 或对象存储读取失败 | 实际执行进程和 Worker 的驱动、端点、权限、凭据与几何编码；Business 默认配置未预置 S3 凭据 |
| 查询超时或内存不足 | 实际生效的执行策略、扫描／连接规模、分区与 Driver／Executor 资源；`LIMIT` 约束输出行数，不能保证前面的聚合或排序计算量随之缩小 |
| 距离或面积明显异常 | 输入 CRS、轴顺序、几何有效性和所用函数单位，不直接换成固定米制标签 |

## 验证与相关文档

本手册不保留缺少源码、数据、配置和运行报告的性能基准。真实验收结论应附对应提交与报告，并按层级区分：

- 确定性 Runtime 契约、分布式 Worker 和平台消费的标准入口见 [Runtime 运行测试](../README.md#运行测试)。
- SQL 工作台和工作流各自的执行与授权边界见 [Develop 模块](../../../develop/CLAUDE.md)及[工作流 HTTP 协议](../../../docs/spec/workflow/HTTP协议.md)。
- 历史验收事实统一见[测试与验收证据索引](../../../docs/spec/testing/验收证据索引.md)，不据此宣称默认 Thrift 已验收 Sedona 空间 SQL。
