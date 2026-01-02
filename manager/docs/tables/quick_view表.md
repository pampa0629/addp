# quick_view 表结构和 API 说明

## 一、表结构概览

`manager.quick_view` 表是 Manager 模块的快显（预缓存）表，记录空间数据表的瓦片预缓存任务状态和配置。支持自动计算最佳缩放级别，按需生成瓦片到 MinIO，实现快速地图渲染。

### 核心功能

- **瓦片预缓存任务管理**：记录快显任务的状态和进度
- **自动层级计算**：根据表的空间范围自动计算 MinZoom 和 MaxZoom
- **停止条件控制**：基于瓦片生成时间和大小自动停止
- **缓存统计追踪**：记录总瓦片数、已缓存数、性能指标

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 快显任务唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `engine_id` | INTEGER | NOT NULL | 引擎 ID |
| `schema_name` | VARCHAR(255) | NOT NULL | Schema 名称 |
| `table_name` | VARCHAR(255) | NOT NULL | 表名 |
| `status` | VARCHAR(50) | NOT NULL, INDEXED | 任务状态 |
| `error_message` | TEXT | | 错误信息（失败时） |
| `min_zoom` | INTEGER | | 最小缩放级别（自动计算） |
| `max_zoom` | INTEGER | DEFAULT 18 | 最大缩放级别 |
| `actual_max_zoom` | INTEGER | | 实际生成到的最大级别 |
| `total_tiles` | INTEGER | DEFAULT 0 | 总瓦片数（估算） |
| `cached_tiles` | INTEGER | DEFAULT 0 | 已缓存瓦片数 |
| `last_zoom_avg_time_ms` | FLOAT | | 最后一层平均生成时间（毫秒） |
| `last_zoom_avg_size_kb` | FLOAT | | 最后一层平均瓦片大小（KB） |
| `stop_threshold_time_ms` | FLOAT | DEFAULT 300 | 停止阈值：生成时间（毫秒） |
| `stop_threshold_size_kb` | FLOAT | DEFAULT 100 | 停止阈值：瓦片大小（KB） |
| `fingerprint` | VARCHAR(64) | NOT NULL, INDEXED | 表指纹（用于 MinIO 路径） |
| `extent` | JSONB | | 空间范围 [minLng, minLat, maxLng, maxLat] |
| `extent_srid` | INTEGER | DEFAULT 4326 | 坐标参考系统 |
| `started_at` | TIMESTAMP | | 任务开始时间 |
| `completed_at` | TIMESTAMP | | 任务完成时间 |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_quick_view_tenant_resource` | tenant_id, engine_id | 按租户和引擎查询 |
| `idx_quick_view_status` | status | 按状态过滤 |
| `idx_quick_view_fingerprint` | fingerprint | 按指纹查询 |

---

## 三、Status 说明

| 值 | 含义 | 说明 |
|---|------|------|
| `none` | 未开始 | 初始状态，未触发快显 |
| `generating` | 生成中 | 正在生成瓦片 |
| `ready` | 就绪 | 瓦片生成完成，可快速渲染 |
| `failed` | 失败 | 生成过程出错 |

---

## 四、JSON 字段详细结构

### 4.1 Extent 字段（空间范围）

**格式**：
```json
[
  73.66, 3.84,  // minLng, minLat (左下角)
  135.05, 53.56 // maxLng, maxLat (右上角)
]
```

**说明**：
- 用于快速计算瓦片范围
- 从表的空间数据自动提取（ST_Extent）
- 坐标系统由 `extent_srid` 指定

---

## 五、API 端点说明

### 5.1 POST /api/engines/:id/spatial/:schema/:table/pre-cache - 触发预缓存

**功能**：触发空间表的瓦片预缓存任务

**请求体**（可选）：

```json
{
  "max_zoom": 18,
  "stop_threshold_time_ms": 300,
  "stop_threshold_size_kb": 100
}
```

**响应**：

```json
{
  "message": "预缓存任务已启动",
  "quick_view_id": 1,
  "status": "generating",
  "min_zoom": 0,
  "max_zoom": 18,
  "estimated_tiles": 1024
}
```

---

### 5.2 GET /api/engines/:id/spatial/:schema/:table/tile-config - 获取瓦片配置

**功能**：获取空间表的瓦片配置（MinZoom、MaxZoom、Extent）

**响应**：

```json
{
  "min_zoom": 0,
  "max_zoom": 18,
  "extent": [73.66, 3.84, 135.05, 53.56],
  "extent_srid": 4326,
  "status": "ready",
  "cached_tiles": 512,
  "total_tiles": 1024
}
```

---

### 5.3 GET /api/engines/:id/spatial/tiles/:schema/:table/:z/:x/:y - 获取瓦片

**功能**：获取指定坐标的 MVT 瓦片（自动使用快显缓存）

**缓存策略**：
1. 内存 LRU 缓存（最快）
2. Redis 缓存（快）
3. MinIO 缓存（中）
4. 实时生成（慢，自动缓存）

**响应**：
- Content-Type: `application/x-protobuf`
- 返回 MVT 格式的瓦片数据

---

## 六、快显任务流程

### 6.1 任务生命周期

```
1. 用户触发预缓存
   ↓
2. 创建 quick_view 记录 (status='none')
   ↓
3. 计算空间范围和 MinZoom/MaxZoom
   ├─ 查询表的 ST_Extent
   ├─ 根据范围计算最佳 MinZoom
   └─ 更新 extent、min_zoom
   ↓
4. 更新状态为 'generating'
   ↓
5. 逐层生成瓦片（从 MinZoom 到 MaxZoom）
   ├─ 每层生成所有覆盖范围的瓦片
   ├─ 保存到 MinIO: mvt-cache/{fingerprint}/{z}/{x}/{y}.mvt
   ├─ 记录性能指标（生成时间、瓦片大小）
   └─ 检查停止条件
   ↓
6. 检查停止条件
   ├─ 瓦片平均生成时间 > threshold_time_ms
   ├─ 或瓦片平均大小 > threshold_size_kb
   └─ 满足任意条件则停止
   ↓
7. 更新状态为 'ready' 或 'failed'
   ├─ completed_at: 完成时间
   └─ actual_max_zoom: 实际生成到的层级
```

### 6.2 停止条件逻辑

**目的**：避免过度缓存（过高层级瓦片数量爆炸）

**条件 1**：生成时间过长
```
last_zoom_avg_time_ms > stop_threshold_time_ms
```

**条件 2**：瓦片过大
```
last_zoom_avg_size_kb > stop_threshold_size_kb
```

**示例**：
- MaxZoom 设置为 18
- 实际生成到 14 层时，发现每个瓦片生成耗时 350ms（超过 300ms 阈值）
- 停止生成，actual_max_zoom = 14

---

## 七、使用示例

### 示例 1：触发预缓存并等待完成

```bash
# 1. 触发预缓存
QUICK_VIEW_ID=$(curl -X POST http://localhost:8081/api/engines/1/spatial/public/cities/pre-cache \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "max_zoom": 18,
    "stop_threshold_time_ms": 300
  }' | jq -r '.quick_view_id')

# 2. 轮询状态
while true; do
  STATUS=$(curl -s -H "Authorization: Bearer $TOKEN" \
    http://localhost:8081/api/engines/1/spatial/public/cities/tile-config \
    | jq -r '.status')
  
  if [ "$STATUS" = "ready" ]; then
    echo "预缓存完成"
    break
  elif [ "$STATUS" = "failed" ]; then
    echo "预缓存失败"
    break
  fi
  
  echo "生成中...进度: $STATUS"
  sleep 2
done
```

### 示例 2：获取瓦片配置

```bash
# 获取瓦片配置（用于前端地图初始化）
curl -X GET http://localhost:8081/api/engines/1/spatial/public/cities/tile-config \
  -H "Authorization: Bearer $TOKEN"
```

---

## 八、重要说明

### 8.1 Fingerprint 设计

**用途**：唯一标识表，用于 MinIO 缓存路径

**生成算法**：
```go
fingerprint := md5(fmt.Sprintf("%d:%s:%s", engineID, schemaName, tableName))
```

**MinIO 路径**：
```
mvt-cache/{fingerprint}/{z}/{x}/{y}.mvt
```

**好处**：
- 避免路径冲突
- 表结构变化时自动失效（重新计算 fingerprint）

### 8.2 自动 MinZoom 计算

**算法**：根据空间范围计算最佳 MinZoom
```go
func CalculateMinZoom(extent [4]float64) int {
    width := extent[2] - extent[0]  // maxLng - minLng
    height := extent[3] - extent[1] // maxLat - minLat
    
    // 假设全球范围为 zoom 0
    // 范围越小，MinZoom 越大
    if width > 180 || height > 90 {
        return 0 // 全球数据
    } else if width > 50 {
        return 3 // 国家级
    } else if width > 10 {
        return 5 // 省级
    } else {
        return 7 // 市级或更小
    }
}
```

### 8.3 缓存失效策略

**触发失效**：
- 表数据更新时（通过 Meta 模块检测）
- 手动清空缓存
- Fingerprint 变化

**实现**：
```bash
# 删除 MinIO 缓存
mc rm --recursive minio/mvt-cache/{fingerprint}/

# 更新 quick_view 记录
UPDATE manager.quick_view 
SET status = 'none', cached_tiles = 0 
WHERE fingerprint = '{fingerprint}';
```

---

## 九、性能监控

### 9.1 生成速度追踪

**指标**：
- `last_zoom_avg_time_ms`：最后一层平均生成时间
- `cached_tiles`：已缓存瓦片数
- `started_at` / `completed_at`：总耗时

**查询统计**：
```sql
-- 查询所有快显任务的性能统计
SELECT 
  schema_name || '.' || table_name AS full_name,
  status,
  cached_tiles,
  EXTRACT(EPOCH FROM (completed_at - started_at)) AS duration_seconds,
  last_zoom_avg_time_ms
FROM manager.quick_view
WHERE status = 'ready'
ORDER BY cached_tiles DESC;
```

### 9.2 存储占用统计

**MinIO 存储**：
```bash
# 查询某个表的缓存大小
mc du minio/mvt-cache/{fingerprint}/
```

---

## 十、相关文档

- [directories表](./directories表.md) - 目录结构表
- [search_history表](./search_history表.md) - 搜索历史表
- [数据库架构](../数据库架构.md) - Manager 模块架构
