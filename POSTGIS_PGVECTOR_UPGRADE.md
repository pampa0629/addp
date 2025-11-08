# PostGIS + pgvector 升级说明

## 更新概述

ADDP系统PostgreSQL已升级为PostGIS镜像,并添加pgvector扩展支持,实现:

- ✅ **PostGIS 空间数据支持** - geometry/geography类型,空间索引,空间函数
- ✅ **pgvector 向量嵌入支持** - vector类型,相似度搜索,多模态AI应用
- ✅ **多架构支持** - ARM64和AMD64自动选择合适镜像

## 镜像版本

### ADDP 系统数据库 (docker-compose.yml)

| 架构 | 镜像 | PostGIS | pgvector |
|------|------|---------|----------|
| ARM64 | `imresamu/postgis-arm64:15-3.4` | ✅ 预装 | ✅ 脚本安装 |
| AMD64 | `postgis/postgis:15-3.4` | ✅ 预装 | ✅ 脚本安装 |

### 业务数据库 (business/docker-compose.yml)

| 架构 | 镜像 | PostGIS | pgvector |
|------|------|---------|----------|
| ARM64 | `imresamu/postgis-arm64:15-3.4` | ✅ 预装 | ❌ 不需要 |
| AMD64 | `postgis/postgis:15-3.4` | ✅ 预装 | ❌ 不需要 |

## 使用方法

### 1. 重启系统基础设施 (启用PostGIS + pgvector)

```bash
# 方法1: 使用集成脚本 (推荐)
cd ~/code/addp
./scripts/infra-restart.sh

# 方法2: 使用Makefile
make infra-restart
```

该脚本会自动:
1. 检测CPU架构 (ARM64/AMD64)
2. 拉取对应的PostGIS镜像
3. 停止并清理旧容器/镜像
4. 启动PostgreSQL容器
5. 安装pgvector扩展

### 2. 重启业务基础设施 (仅启用PostGIS)

```bash
cd ~/code/addp/business
./scripts/restart.sh
```

### 3. 验证扩展安装

```bash
# 查看已安装扩展
docker exec addp-postgres psql -U addp -d addp -c '\dx'

# 验证PostGIS版本
docker exec addp-postgres psql -U addp -d addp -c 'SELECT PostGIS_Version();'

# 验证pgvector版本
docker exec addp-postgres psql -U addp -d addp -c "SELECT extversion FROM pg_extension WHERE extname = 'vector';"
```

预期输出:
```
                                      List of installed extensions
       Name       | Version |   Schema   |                        Description
------------------+---------+------------+------------------------------------------------------------
 plpgsql          | 1.0     | pg_catalog | PL/pgSQL procedural language
 postgis          | 3.4.x   | public     | PostGIS geometry and geography spatial types and functions
 postgis_topology | 3.4.x   | topology   | PostGIS topology spatial types and functions
 vector           | 0.7.0   | public     | vector data type and ivfflat and hnsw access methods
```

## PostGIS 使用示例

### 空间数据类型

```sql
-- 创建空间表
CREATE TABLE locations (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255),
    geom geometry(Point, 4326)  -- WGS84坐标系的点
);

-- 插入空间数据 (经度, 纬度)
INSERT INTO locations (name, geom) VALUES
('北京', ST_SetSRID(ST_MakePoint(116.4074, 39.9042), 4326)),
('上海', ST_SetSRID(ST_MakePoint(121.4737, 31.2304), 4326));

-- 空间查询: 查找距离北京10km内的位置
SELECT name, ST_Distance(
    geom,
    (SELECT geom FROM locations WHERE name = '北京')
) AS distance_meters
FROM locations
WHERE ST_DWithin(
    geom,
    (SELECT geom FROM locations WHERE name = '北京'),
    10000  -- 10km in meters
);
```

### 空间索引

```sql
-- 创建空间索引 (GiST)
CREATE INDEX idx_locations_geom ON locations USING GIST (geom);
```

## pgvector 使用示例

### 向量嵌入存储

```sql
-- 创建向量表 (OpenAI text-embedding-3-small: 1536维)
CREATE TABLE documents (
    id SERIAL PRIMARY KEY,
    content TEXT,
    embedding vector(1536)  -- 向量维度必须匹配嵌入模型
);

-- 插入向量数据
INSERT INTO documents (content, embedding) VALUES
('人工智能是计算机科学的一个分支', '[0.1, 0.2, 0.3, ...]'::vector),
('机器学习是AI的核心技术', '[0.15, 0.25, 0.35, ...]'::vector);

-- 相似度搜索 (余弦距离,值越小越相似)
SELECT content, embedding <=> '[0.12, 0.22, 0.32, ...]'::vector AS cosine_distance
FROM documents
ORDER BY embedding <=> '[0.12, 0.22, 0.32, ...]'::vector
LIMIT 5;
```

### 向量索引 (加速大规模搜索)

```sql
-- IVFFlat 索引 (适合中等规模数据,< 100万条)
CREATE INDEX ON documents USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- HNSW 索引 (适合大规模数据,> 100万条,更高精度)
CREATE INDEX ON documents USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
```

### 距离度量

pgvector支持三种距离运算符:

| 运算符 | 距离类型 | 说明 | 适用场景 |
|--------|----------|------|----------|
| `<->` | L2距离 (欧几里得) | 向量空间中的直线距离 | 图像嵌入 |
| `<=>` | 余弦距离 | 1 - 余弦相似度 | 文本嵌入 (最常用) |
| `<#>` | 内积 (负值) | 负的点积 | 推荐系统 |

## 多模态AI应用场景

### 1. 文本语义搜索

```sql
-- 存储文档嵌入 (OpenAI, Cohere, etc.)
CREATE TABLE articles (
    id SERIAL PRIMARY KEY,
    title TEXT,
    content TEXT,
    embedding vector(1536)  -- text-embedding-3-small
);

-- 查询相似文章
SELECT title, content
FROM articles
ORDER BY embedding <=> $1::vector  -- 查询文本的嵌入向量
LIMIT 10;
```

### 2. 图像相似度搜索

```sql
-- 存储图像嵌入 (CLIP, ResNet, etc.)
CREATE TABLE images (
    id SERIAL PRIMARY KEY,
    url TEXT,
    embedding vector(512)  -- CLIP ViT-B/32
);

-- 查找相似图片
SELECT url
FROM images
ORDER BY embedding <-> $1::vector  -- 查询图像的嵌入向量
LIMIT 20;
```

### 3. 混合空间-向量搜索 (PostGIS + pgvector)

```sql
-- 结合地理位置和语义相似度
CREATE TABLE poi (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    location geometry(Point, 4326),
    description_embedding vector(1536)
);

-- 查询: 附近的语义相关地点
SELECT name, description,
    ST_Distance(location, ST_SetSRID(ST_MakePoint(116.4, 39.9), 4326)) AS distance,
    description_embedding <=> $1::vector AS semantic_similarity
FROM poi
WHERE ST_DWithin(location, ST_SetSRID(ST_MakePoint(116.4, 39.9), 4326), 5000)  -- 5km内
ORDER BY description_embedding <=> $1::vector  -- 按语义相似度排序
LIMIT 10;
```

## 性能优化建议

### PostGIS

1. **创建空间索引** - 所有geometry/geography列都应该创建GiST索引
2. **选择合适的SRID** - 使用WGS84 (4326)或Web Mercator (3857)
3. **简化几何** - 对复杂多边形使用`ST_Simplify()`减少顶点数
4. **启用并行查询** - `SET max_parallel_workers_per_gather = 4;`

### pgvector

1. **创建向量索引**:
   - 数据量 < 10万: 不需要索引 (顺序扫描足够快)
   - 数据量 10万-100万: 使用IVFFlat索引
   - 数据量 > 100万: 使用HNSW索引

2. **调整索引参数**:
   ```sql
   -- IVFFlat: lists = sqrt(总行数)
   CREATE INDEX ON table USING ivfflat (embedding vector_cosine_ops)
   WITH (lists = 1000);  -- 对于100万行数据

   -- HNSW: m越大召回率越高但索引越大
   CREATE INDEX ON table USING hnsw (embedding vector_cosine_ops)
   WITH (m = 16, ef_construction = 64);
   ```

3. **查询优化**:
   ```sql
   -- 设置查询时的候选数量 (值越大精度越高但速度越慢)
   SET ivfflat.probes = 10;  -- 默认1
   SET hnsw.ef_search = 40;  -- 默认40
   ```

## 故障排查

### PostGIS扩展创建失败

```bash
# 查看容器日志
docker logs addp-postgres

# 手动创建扩展
docker exec addp-postgres psql -U addp -d addp -c "CREATE EXTENSION IF NOT EXISTS postgis;"
```

### pgvector编译失败

```bash
# 检查容器内是否有编译工具
docker exec addp-postgres which gcc

# 手动运行安装脚本
./scripts/infra-init-pgvector.sh
```

### 架构不匹配错误

```bash
# 检查镜像架构
docker image inspect imresamu/postgis-arm64:15-3.4 --format '{{.Architecture}}'

# 强制拉取ARM64镜像
docker pull --platform=linux/arm64 imresamu/postgis-arm64:15-3.4
```

## 相关文档

- [PostGIS官方文档](https://postgis.net/documentation/)
- [pgvector官方文档](https://github.com/pgvector/pgvector)
- [PostgreSQL空间数据库](https://www.postgresql.org/docs/15/gist.html)
- [向量相似度搜索最佳实践](https://github.com/pgvector/pgvector#best-practices)

## 更新日志

- **2025-01-07**: 初始版本
  - ADDP系统PostgreSQL升级为PostGIS镜像 (ARM64: imresamu, AMD64: postgis官方)
  - 添加pgvector扩展支持 (v0.7.0)
  - 创建`infra-restart.sh`和`infra-init-pgvector.sh`脚本
  - 业务PostgreSQL同步升级为PostGIS镜像
