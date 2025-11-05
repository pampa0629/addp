# PostGIS 空间扩展配置说明

本文档说明业务 PostgreSQL 数据库的 PostGIS 空间扩展配置和使用方法。

## 自动安装配置

Business 基础设施已配置为自动安装和启用 PostGIS 扩展。

### 配置文件修改清单

以下文件已更新以支持 PostGIS：

1. ✅ **docker-compose.yml** - 使用 PostGIS 镜像
   ```yaml
   image: postgis/postgis:15-3.4
   ```

2. ✅ **docker-compose.prod.yml** - 生产环境 PostGIS 镜像
   ```yaml
   image: ${REGISTRY:-localhost:5000}/addp-infra-postgres-postgis:15-3.4
   ```

3. ✅ **init-db.sql** - 数据库初始化时自动安装扩展
   ```sql
   CREATE EXTENSION IF NOT EXISTS postgis;
   CREATE EXTENSION IF NOT EXISTS postgis_topology;
   ```

4. ✅ **scripts/start.sh** - 启动脚本自动调用 PostGIS 安装
5. ✅ **scripts/install-postgis.sh** - 独立的 PostGIS 安装脚本
6. ✅ **README.md** - 添加 PostGIS 使用说明
7. ✅ **ARCHITECTURE.md** - 添加空间数据架构说明
8. ✅ **DEPLOY.md** - 添加部署验证步骤

## PostGIS 镜像说明

### 开发环境 (docker-compose.yml)
```yaml
image: postgis/postgis:15-3.4
```

### 生产环境 (docker-compose.prod.yml)

生产环境使用私有镜像仓库：
```yaml
image: ${REGISTRY:-localhost:5000}/addp-infra-postgres-postgis:15-3.4
```

**镜像构建方法**：
```bash
# 拉取官方镜像
docker pull postgis/postgis:15-3.4

# 标记为私有仓库镜像
docker tag postgis/postgis:15-3.4 localhost:5000/addp-infra-postgres-postgis:15-3.4

# 推送到私有仓库
docker push localhost:5000/addp-infra-postgres-postgis:15-3.4
```

## 验证 PostGIS 安装

### 方法一：使用验证脚本

```bash
cd business
./scripts/install-postgis.sh
```

输出示例：
```
========================================
  PostGIS Extension Installation
========================================

Checking PostGIS installation...
✓ PostGIS is already installed
  Version: POSTGIS="3.4.0" ...

Checking PostGIS Topology extension...
✓ PostGIS Topology is already installed

========================================
  PostGIS Installation Complete!
========================================
```

### 方法二：手动检查

```bash
# 连接到数据库
docker exec -it business-postgres psql -U business -d business

# 查看已安装的扩展
\dx

# 应该看到：
# postgis | 3.4.0 | public | PostGIS geometry and geography spatial types and functions
# postgis_topology | 3.4.0 | topology | PostGIS topology spatial types and functions

# 查看 PostGIS 版本
SELECT PostGIS_Version();

# 查看完整版本信息
SELECT PostGIS_Full_Version();

# 测试空间查询
SELECT ST_AsText(ST_GeomFromText('POINT(116.4 39.9)'));
```

## 支持的空间数据类型

PostGIS 扩展支持以下空间数据类型：

### 基础类型
- **GEOMETRY** - 通用几何类型（最常用）
- **GEOGRAPHY** - 地理类型（使用球面坐标）

### 具体几何类型
- **POINT** - 点（经度,纬度）
- **LINESTRING** - 线段（路径、边界）
- **POLYGON** - 多边形（区域、面）
- **MULTIPOINT** - 多点集合
- **MULTILINESTRING** - 多线段集合
- **MULTIPOLYGON** - 多多边形集合
- **GEOMETRYCOLLECTION** - 几何集合

### 拓扑类型（需要 postgis_topology 扩展）
- **TOPOGEOMETRY** - 拓扑几何类型

## Transfer 模块导入空间数据

在 Transfer 模块中导入空间数据时，需要指定目标 schema：

### 导入 Shapefile 到 PostgreSQL

1. **在 Transfer 任务向导中配置**：
   - 数据源类型：MinIO（Shapefile）
   - 目标数据源类型：PostgreSQL（Business Database）
   - 目标表名：`business_data.spatial_layer_name`
     - ⚠️ **必须包含 schema 前缀**（如 `business_data.`）
   - 自动建表：启用
   - 写入模式：INSERT

2. **系统自动处理**：
   - 创建 `business_data` schema（如果不存在）
   - 创建表结构（包含 GEOMETRY 列）
   - 导入数据行
   - 创建空间索引（如果配置）

### 支持的导入格式

| 格式 | Reader 插件 | 空间字段处理 |
|------|-------------|-------------|
| Shapefile (.shp) | ShapefileReader | 自动转换为 GEOMETRY |
| GeoJSON | GeoJSONReader | 自动解析 geometry 字段 |
| WKT/WKB | JDBCReader | 使用 `ST_GeomFromText()` |
| PostgreSQL (含空间列) | JDBCReader | 直接复制 GEOMETRY 列 |

### 示例：北京行政区划导入

```yaml
任务配置：
  源：
    - 类型：MinIO
    - Bucket：gis-data
    - 文件：beijing/districts.shp
  目标：
    - 类型：PostgreSQL（Business）
    - 主机：business-postgres
    - 数据库：business
    - Schema：business_data
    - 表：beijing_districts
  配置：
    - 批量大小：1000
    - 自动建表：是
    - 写入模式：INSERT
    - 冲突策略：SKIP
```

导入后的表结构：
```sql
CREATE TABLE business_data.beijing_districts (
    gid SERIAL PRIMARY KEY,
    name VARCHAR(255),
    district_code VARCHAR(50),
    population INTEGER,
    geom GEOMETRY(MULTIPOLYGON, 4326)
);

CREATE INDEX idx_beijing_districts_geom
ON business_data.beijing_districts
USING GIST (geom);
```

## 空间查询示例

### 基础查询

```sql
-- 查找某个点附近的区域
SELECT name, ST_AsText(geom)
FROM business_data.beijing_districts
WHERE ST_DWithin(
    geom,
    ST_SetSRID(ST_MakePoint(116.4, 39.9), 4326),
    0.1  -- 0.1度 ≈ 11km
);

-- 计算区域面积（平方米）
SELECT name, ST_Area(geom::geography) / 1000000 AS area_km2
FROM business_data.beijing_districts
ORDER BY area_km2 DESC;

-- 查找相邻区域
SELECT a.name AS district1, b.name AS district2
FROM business_data.beijing_districts a
JOIN business_data.beijing_districts b
ON ST_Touches(a.geom, b.geom)
WHERE a.gid < b.gid;
```

### 高级查询

```sql
-- 缓冲区分析（周边1km范围）
SELECT name, ST_AsGeoJSON(ST_Buffer(geom::geography, 1000)::geometry)
FROM business_data.beijing_districts
WHERE name = '朝阳区';

-- 空间连接（点在面内）
SELECT p.poi_name, d.district_name
FROM business_data.beijing_pois p
JOIN business_data.beijing_districts d
ON ST_Within(p.geom, d.geom);

-- 最近邻查询
SELECT name, ST_Distance(geom::geography,
    ST_SetSRID(ST_MakePoint(116.4, 39.9), 4326)::geography
) AS distance_meters
FROM business_data.beijing_districts
ORDER BY distance_meters
LIMIT 5;
```

## 故障排查

### 问题 1：PostGIS 扩展未安装

**症状**：
```sql
ERROR: type "geometry" does not exist
```

**解决方案**：
```bash
# 运行安装脚本
cd business
./scripts/install-postgis.sh

# 或手动安装
docker exec business-postgres psql -U business -d business -c "CREATE EXTENSION IF NOT EXISTS postgis;"
```

### 问题 2：空间索引缺失导致查询慢

**症状**：空间查询耗时很长

**解决方案**：
```sql
-- 创建空间索引
CREATE INDEX idx_tablename_geom
ON business_data.tablename
USING GIST (geom);

-- 分析表以更新统计信息
ANALYZE business_data.tablename;
```

### 问题 3：坐标系不匹配

**症状**：
```sql
ERROR: Operation on mixed SRID geometries
```

**解决方案**：
```sql
-- 检查坐标系
SELECT ST_SRID(geom) FROM business_data.tablename LIMIT 1;

-- 转换坐标系
UPDATE business_data.tablename
SET geom = ST_Transform(ST_SetSRID(geom, 4326), 3857)
WHERE ST_SRID(geom) = 0;

-- 或在查询时转换
SELECT ST_Transform(geom, 4326) FROM business_data.tablename;
```

### 问题 4：镜像拉取失败

**症状**：
```
Error: docker.io/postgis/postgis:15-3.4: not found
```

**解决方案 1：使用国内镜像源**
```bash
# 配置 Docker 镜像加速器
sudo vim /etc/docker/daemon.json
{
  "registry-mirrors": [
    "https://mirror.ccs.tencentyun.com",
    "https://docker.m.daocloud.io"
  ]
}

sudo systemctl restart docker
```

**解决方案 2：使用其他可用标签**
```bash
# 尝试其他版本
docker pull postgis/postgis:16-3.4
docker pull postgis/postgis:14-3.3

# 查看可用标签
curl -s https://hub.docker.com/v2/repositories/postgis/postgis/tags | jq -r '.results[].name' | grep "^15-"
```

**解决方案 3：构建自定义镜像**
```dockerfile
# Dockerfile.business-postgres
FROM postgres:15-alpine

# 安装 PostGIS 依赖
RUN apk add --no-cache postgis

# 复制初始化脚本
COPY init-db.sql /docker-entrypoint-initdb.d/

# 构建镜像
docker build -t business-postgres:15-postgis -f Dockerfile.business-postgres .
```

## 性能优化建议

### 1. 创建空间索引
```sql
CREATE INDEX idx_geom ON business_data.tablename USING GIST (geom);
```

### 2. 使用简化几何
```sql
-- 对复杂多边形进行简化（容差0.0001度 ≈ 11米）
SELECT name, ST_Simplify(geom, 0.0001) AS simplified_geom
FROM business_data.complex_polygons;
```

### 3. 启用并行查询
```sql
SET max_parallel_workers_per_gather = 4;
SET parallel_setup_cost = 100;
SET parallel_tuple_cost = 0.01;
```

### 4. 调整 PostgreSQL 配置
```bash
# 在 docker-compose.yml 中添加
environment:
  - POSTGRES_SHARED_BUFFERS=256MB
  - POSTGRES_EFFECTIVE_CACHE_SIZE=1GB
  - POSTGRES_WORK_MEM=16MB
```

## 相关资源

- [PostGIS 官方文档](https://postgis.net/documentation/)
- [PostGIS 函数参考](https://postgis.net/docs/reference.html)
- [Transfer 模块空间数据导入文档](../transfer/docs/SPATIAL_DATA.md)
- [业务基础设施架构文档](./ARCHITECTURE.md)

## 附录：常用 PostGIS 函数

| 函数 | 说明 | 示例 |
|------|------|------|
| `ST_GeomFromText()` | 从 WKT 创建几何 | `ST_GeomFromText('POINT(116.4 39.9)', 4326)` |
| `ST_AsText()` | 转换为 WKT | `ST_AsText(geom)` |
| `ST_AsGeoJSON()` | 转换为 GeoJSON | `ST_AsGeoJSON(geom)` |
| `ST_Distance()` | 计算距离 | `ST_Distance(geom1, geom2)` |
| `ST_Within()` | 判断是否在内部 | `ST_Within(point, polygon)` |
| `ST_Intersects()` | 判断是否相交 | `ST_Intersects(geom1, geom2)` |
| `ST_Buffer()` | 创建缓冲区 | `ST_Buffer(geom, 1000)` |
| `ST_Area()` | 计算面积 | `ST_Area(geom::geography)` |
| `ST_Length()` | 计算长度 | `ST_Length(linestring::geography)` |
| `ST_Centroid()` | 计算中心点 | `ST_Centroid(polygon)` |
| `ST_Transform()` | 坐标系转换 | `ST_Transform(geom, 3857)` |
| `ST_Simplify()` | 简化几何 | `ST_Simplify(geom, 0.0001)` |
