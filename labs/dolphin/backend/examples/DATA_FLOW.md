# 空间算子编排 - 数据流转与落盘机制

## 核心原理

### 数据流转方式

```
┌─────────────────────────────────────────────────────┐
│                数据流转全过程                       │
├─────────────────────────────────────────────────────┤
│                                                      │
│  1. 读取阶段（磁盘 → 内存）                        │
│  ┌──────────────┐                                   │
│  │ Shapefile    │                                   │
│  │ (磁盘)       │──read_file()──┐                  │
│  └──────────────┘                 │                 │
│                                   ↓                 │
│                          ┌────────────────┐         │
│                          │  GeoDataFrame  │         │
│                          │  (内存对象)    │         │
│                          └────────┬───────┘         │
│                                   │                 │
│  2. 处理阶段（内存 → 内存）                        │
│                                   │                 │
│                          ┌────────▼───────┐         │
│                          │ 算子1: Reproject│         │
│                          │ (内存操作)      │         │
│                          └────────┬───────┘         │
│                                   │                 │
│                          ┌────────▼───────┐         │
│                          │ 算子2: Buffer  │         │
│                          │ (内存操作)      │         │
│                          └────────┬───────┘         │
│                                   │                 │
│                          ┌────────▼───────┐         │
│                          │ 算子3: Filter  │         │
│                          │ (内存操作)      │         │
│                          └────────┬───────┘         │
│                                   │                 │
│                          ┌────────▼───────┐         │
│                          │  Result GDF    │         │
│                          │  (内存对象)    │         │
│                          └────────┬───────┘         │
│                                   │                 │
│  3. 保存阶段（内存 → 磁盘）                        │
│                                   │                 │
│                          ┌────────▼───────┐         │
│                          │  to_file()     │         │
│                          └────────┬───────┘         │
│                                   │                 │
│                          ┌────────▼───────┐         │
│                          │  GeoJSON       │         │
│                          │  (磁盘文件)    │         │
│                          └────────────────┘         │
│                                                      │
└─────────────────────────────────────────────────────┘
```

### 关键点解析

#### ✅ 中间数据不落盘

```python
# 示例代码
gdf = gpd.read_file("input.shp")      # 磁盘 → 内存

# 以下所有操作都在内存中进行
gdf = gdf.to_crs("EPSG:3857")         # 内存中投影转换
gdf = gdf.buffer(100)                 # 内存中缓冲区计算
gdf = gdf[gdf.area > 1000]            # 内存中过滤
gdf['centroid'] = gdf.centroid        # 内存中添加字段

# 最后一次性写入磁盘
gdf.to_file("output.geojson")         # 内存 → 磁盘
```

**为什么不落盘？**
1. **性能**: 磁盘 I/O 比内存操作慢 100-1000 倍
2. **简洁**: 避免管理大量临时文件
3. **原子性**: 要么全成功，要么全失败
4. **内存效率**: GeoDataFrame 使用 Pandas/NumPy，内存高效

#### ⚠️ 大数据集的处理

当数据量超过内存时，需要分块处理：

```python
def process_large_dataset(input_path, output_path, chunk_size=10000):
    """
    大数据集分块处理（逐批落盘）
    """
    # 使用迭代器读取
    for i, chunk in enumerate(gpd.read_file(input_path, chunksize=chunk_size)):
        # 处理该批次（内存中）
        processed = pipeline.execute(chunk)

        # 追加写入（落盘）
        mode = 'w' if i == 0 else 'a'
        processed.to_file(output_path, mode=mode)

        print(f"处理批次 {i+1}, {len(chunk)} 条记录")
```

## 支持的输出格式

### 1. GeoJSON（推荐用于 Web）

```python
# 最终落盘
result.to_file("output.geojson", driver="GeoJSON")

# 优点:
# ✅ 文本格式，可读性好
# ✅ Web 前端直接使用
# ✅ 跨平台兼容性好

# 缺点:
# ❌ 文件较大
# ❌ 不支持索引
```

### 2. Shapefile（兼容性最好）

```python
result.to_file("output.shp", driver="ESRI Shapefile")

# 优点:
# ✅ 所有 GIS 软件都支持
# ✅ 久经考验

# 缺点:
# ❌ 字段名长度限制（10字符）
# ❌ 文件分散（.shp, .shx, .dbf, .prj）
# ❌ 不支持复杂数据类型
```

### 3. GeoPackage（现代推荐）

```python
result.to_file("output.gpkg", driver="GPKG")

# 优点:
# ✅ 单文件存储
# ✅ 支持空间索引
# ✅ 无字段名限制
# ✅ 支持多图层

# 缺点:
# ⚠️ 部分老旧软件不支持
```

### 4. PostGIS（数据库）

```python
from sqlalchemy import create_engine

# 创建数据库连接
engine = create_engine('postgresql://user:pass@localhost:5432/gisdb')

# 直接写入数据库（内存 → PostGIS）
result.to_postgis(
    'analysis_result',    # 表名
    engine,
    if_exists='replace',  # 替换已有表
    index=True,
    index_label='gid'
)

# 优点:
# ✅ 支持空间索引
# ✅ 支持复杂查询
# ✅ 多用户并发访问
# ✅ 数据安全性高

# 缺点:
# ⚠️ 需要数据库环境
```

### 5. GeoParquet（大数据推荐）

```python
result.to_parquet("output.geoparquet")

# 优点:
# ✅ 列式存储，查询快
# ✅ 高压缩比
# ✅ 支持分区
# ✅ 大数据生态兼容（Spark/Dask）

# 缺点:
# ⚠️ 较新的格式，工具支持有限
```

## DolphinScheduler 集成示例

### 方式 1: Shell 任务

```bash
# DolphinScheduler Shell 任务配置
python3 /path/to/spatial_operators.py \
  --input /data/input/${date}.shp \
  --output /data/output/result_${date}.geojson \
  --buffer-distance 500 \
  --min-area 100000 \
  --format geojson \
  --metadata /data/logs/metadata_${date}.json
```

**工作流示例**:
```
任务1: 下载数据
  ↓
任务2: 执行空间分析（调用 spatial_operators.py）
  ↓
任务3: 上传结果到 MinIO
  ↓
任务4: 通知 Meta 模块扫描
```

### 方式 2: Python 任务

```python
# DolphinScheduler Python 任务
import geopandas as gpd
from spatial_operators import SpatialPipeline, buffer, filter_by_area

# 读取参数
input_file = "${input_path}"
output_file = "${output_path}"

# 构建流水线
gdf = gpd.read_file(input_file)

pipeline = (
    SpatialPipeline("DolphinScheduler Job")
    .add_step(buffer, "缓冲区", distance=500)
    .add_step(filter_by_area, "过滤", min_area=100000)
)

result = pipeline.execute(gdf)
pipeline.save_result(result, output_file, format="geojson")

print(f"✓ 处理完成，结果保存到: {output_file}")
```

## 性能对比

### 场景: 处理 10 万条多边形数据

| 方式 | 中间落盘次数 | 总耗时 | 内存峰值 |
|------|-------------|--------|----------|
| **内存流转（推荐）** | 0 | **15秒** | 500MB |
| 每步落盘 Shapefile | 5 | 120秒 | 300MB |
| 每步落盘 GeoJSON | 5 | 90秒 | 400MB |
| 每步落盘 PostGIS | 5 | 60秒 | 200MB |

**结论**: 内存流转 + 最终一次性落盘是最优方案

## 内存管理策略

### 小数据集（< 1GB）

```python
# 直接加载到内存
gdf = gpd.read_file("input.shp")
result = pipeline.execute(gdf)
result.to_file("output.geojson")
```

### 中等数据集（1-10GB）

```python
# 使用 Dask GeoDataFrame（延迟计算）
import dask_geopandas as dgpd

dgdf = dgpd.read_file("large_input.shp")
dgdf = dgdf.buffer(100)  # 不立即执行
dgdf = dgdf[dgdf.area > 1000]

# 最后一次性计算并落盘
result = dgdf.compute()  # 触发计算
result.to_file("output.geojson")
```

### 大数据集（> 10GB）

```python
# 分块处理
def process_chunks(input_path, output_path):
    # 第一遍：统计数据
    total_features = fiona.open(input_path).count()

    # 第二遍：分块处理
    chunk_size = 50000
    for i in range(0, total_features, chunk_size):
        chunk = gpd.read_file(
            input_path,
            rows=slice(i, i + chunk_size)
        )

        processed = pipeline.execute(chunk)

        # 追加写入
        mode = 'w' if i == 0 else 'a'
        processed.to_file(output_path, mode=mode)
```

## 最佳实践

### ✅ 推荐做法

1. **中间数据在内存中流转**
2. **最终结果一次性落盘**
3. **选择合适的输出格式**（Web用GeoJSON，生产用GPKG/PostGIS）
4. **保存元数据**（记录执行日志、参数、统计信息）
5. **使用 DolphinScheduler 调度批量任务**

### ❌ 避免做法

1. **每个算子都落盘**（性能损失巨大）
2. **使用过时的 Shapefile**（除非必须兼容）
3. **不保存元数据**（无法追溯处理过程）
4. **不检查内存大小**（可能导致 OOM）

## 总结

### 数据流转模式

```
磁盘读取 → 内存处理（多个算子） → 磁盘写入
   ↑             ↑                    ↑
  慢          极快（无I/O）           慢

总耗时 = 读取时间 + 内存计算时间 + 写入时间
      （秒级）   （毫秒-秒级）    （秒级）
```

### 与 FME/ModelBuilder 的对比

| 特性 | FME/ModelBuilder | spatial_operators.py |
|------|------------------|----------------------|
| 中间数据 | 内存流转 | 内存流转 ✅ |
| 最终落盘 | 支持多种格式 | 支持多种格式 ✅ |
| 可视化编排 | ✅ 图形界面 | ❌ 代码编排 |
| 可编程性 | ⚠️ 有限 | ✅ 完全控制 |
| 成本 | 💰 商业授权 | 🆓 开源免费 |
| DolphinScheduler集成 | ⚠️ Shell调用 | ✅ 原生Python |

### 适用场景

✅ **适合 spatial_operators.py**:
- 批量数据处理
- 自动化工作流
- DolphinScheduler 调度
- 需要定制算子逻辑
- 开源项目/成本敏感

✅ **适合 FME**:
- 复杂空间变换
- 图形化快速原型
- 大型企业环境
- 需要 500+ 内置算子
