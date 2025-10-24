# Spatial Transform 使用指南

## 功能概述

Transfer 模块现已支持 **空间数据类型转换** 和 **通用转换器扩展机制**，可以在数据传输过程中对空间几何数据进行格式转换。

### 核心功能

✅ **已实现 (P0)**:
- `TransformRegistry` - 转换器注册表和插件管理
- `SpatialTransform` - 空间数据格式转换器
- Transform API 端点 - RESTful API 接口
- 完整的单元测试覆盖

✅ **支持的空间数据格式**:
- WKB (Well-Known Binary)
- WKT (Well-Known Text)
- GeoJSON
- EWKB (Extended WKB - PostGIS)
- EWKT (Extended WKT)
- HexWKB (Hex-encoded WKB)

✅ **支持的几何类型**:
- Point (点)
- LineString (线)
- Polygon (多边形)
- MultiPoint (多点)
- MultiLineString (多线)
- MultiPolygon (多多边形)
- GeometryCollection (几何集合)

---

## API 端点

### 1. 列出所有可用转换器

```bash
GET /api/transforms
```

**响应示例**:
```json
{
  "total": 1,
  "transforms": [
    {
      "name": "spatial",
      "description": "Transform spatial/geometry data between different formats",
      "supported_types": ["geometry", "geography", "blob", "binary", "bytea"],
      "config_schema": { ... },
      "version": "1.0.0",
      "author": "ADDP Transfer Module"
    }
  ]
}
```

### 2. 获取转换器详细信息

```bash
GET /api/transforms/spatial
```

**响应示例**:
```json
{
  "name": "spatial",
  "description": "Transform spatial/geometry data...",
  "supported_types": ["geometry", "geography", "blob", "binary", "bytea"],
  "config_schema": {
    "type": "object",
    "required": ["geometry_fields"],
    "properties": {
      "geometry_fields": {
        "type": "array",
        "description": "List of geometry field names to transform",
        "items": { "type": "string" },
        "minItems": 1
      },
      "source_format": {
        "type": "string",
        "enum": ["wkb", "wkt", "ewkb", "ewkt", "geojson", "hexwkb"],
        "default": "wkb"
      },
      "target_format": {
        "type": "string",
        "enum": ["wkb", "wkt", "ewkb", "ewkt", "geojson", "hexwkb"],
        "default": "wkb"
      },
      "source_srid": {
        "type": "integer",
        "description": "Source SRID (e.g., 4326 for WGS84)"
      },
      "target_srid": {
        "type": "integer",
        "description": "Target SRID (e.g., 3857 for Web Mercator)"
      }
    }
  }
}
```

### 3. 验证转换器配置

```bash
POST /api/transforms/spatial/validate
Content-Type: application/json

{
  "config": {
    "geometry_fields": ["geom"],
    "source_format": "wkb",
    "target_format": "wkt"
  }
}
```

**响应示例**:
```json
{
  "valid": true
}
```

### 4. 测试转换器

```bash
POST /api/transforms/spatial/test
Content-Type: application/json

{
  "config": {
    "geometry_fields": ["location"],
    "source_format": "wkt",
    "target_format": "geojson"
  },
  "sample": [
    {
      "id": 1,
      "name": "Beijing",
      "location": "POINT (116.4074 39.9042)"
    }
  ]
}
```

**响应示例**:
```json
{
  "success": true,
  "input": [
    {
      "id": 1,
      "name": "Beijing",
      "location": "POINT (116.4074 39.9042)"
    }
  ],
  "output": [
    {
      "id": 1,
      "name": "Beijing",
      "location": {
        "type": "Point",
        "coordinates": [116.4074, 39.9042]
      }
    }
  ]
}
```

### 5. 获取转换器统计信息

```bash
GET /api/transforms/stats
```

**响应示例**:
```json
{
  "total_transforms": 1,
  "supported_types": {
    "geometry": 1,
    "geography": 1,
    "blob": 1,
    "binary": 1,
    "bytea": 1
  },
  "available_transforms": ["spatial"]
}
```

---

## 使用场景

### 场景 1: PostGIS 到 MySQL Spatial 数据迁移

PostGIS 使用 EWKB 格式，MySQL Spatial 使用标准 WKB，需要格式转换。

**创建任务配置**:
```json
{
  "name": "PostGIS to MySQL Spatial Migration",
  "type": "sync",
  "source_id": 1,  // PostGIS 数据源
  "target_id": 2,  // MySQL 数据源
  "config": {
    "source": {
      "query": "SELECT id, name, ST_AsBinary(geom) as geom FROM cities"
    },
    "target": {
      "table": "cities"
    },
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["geom"],
        "source_format": "wkb",
        "target_format": "wkb",
        "validate_geometry": true
      }
    ]
  }
}
```

### 场景 2: 数据库到 GeoJSON API 导出

将数据库中的空间数据导出为 GeoJSON 格式供前端使用。

**任务配置**:
```json
{
  "name": "Export to GeoJSON",
  "type": "export",
  "source_id": 1,
  "config": {
    "source": {
      "table": "landmarks",
      "query": "SELECT id, name, ST_AsText(location) as location FROM landmarks"
    },
    "target": {
      "type": "file",
      "path": "/data/exports/landmarks.geojson"
    },
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["location"],
        "source_format": "wkt",
        "target_format": "geojson"
      }
    ]
  }
}
```

### 场景 3: 多个几何字段转换

一个表包含多个几何字段（起点、终点、路径）。

**任务配置**:
```json
{
  "name": "Multi-geometry Field Conversion",
  "type": "sync",
  "config": {
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["start_point", "end_point", "route"],
        "source_format": "wkb",
        "target_format": "wkt"
      }
    ]
  }
}
```

### 场景 4: HexWKB 格式转换

某些系统（如 SQLite Spatialite）使用 Hex-encoded WKB 格式。

**任务配置**:
```json
{
  "config": {
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["geom"],
        "source_format": "hexwkb",
        "target_format": "geojson"
      }
    ]
  }
}
```

---

## 前端集成示例

### Vue 3 组件示例

```vue
<template>
  <div class="spatial-transform-config">
    <h3>空间数据转换配置</h3>

    <!-- 转换器选择 -->
    <el-select v-model="selectedTransform" placeholder="选择转换器">
      <el-option
        v-for="t in availableTransforms"
        :key="t.name"
        :label="t.name"
        :value="t.name"
      />
    </el-select>

    <!-- 动态配置表单 -->
    <el-form v-if="transformCapability" :model="config">
      <el-form-item label="几何字段">
        <el-select v-model="config.geometry_fields" multiple>
          <el-option
            v-for="field in geometryFields"
            :key="field"
            :value="field"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="源格式">
        <el-select v-model="config.source_format">
          <el-option value="wkb" label="WKB" />
          <el-option value="wkt" label="WKT" />
          <el-option value="geojson" label="GeoJSON" />
          <el-option value="hexwkb" label="HexWKB" />
        </el-select>
      </el-form-item>

      <el-form-item label="目标格式">
        <el-select v-model="config.target_format">
          <el-option value="wkb" label="WKB" />
          <el-option value="wkt" label="WKT" />
          <el-option value="geojson" label="GeoJSON" />
          <el-option value="hexwkb" label="HexWKB" />
        </el-select>
      </el-form-item>

      <!-- 测试按钮 -->
      <el-button @click="testTransform">测试转换</el-button>
    </el-form>

    <!-- 测试结果 -->
    <div v-if="testResult">
      <h4>测试结果</h4>
      <pre>{{ JSON.stringify(testResult, null, 2) }}</pre>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import axios from 'axios';

const availableTransforms = ref([]);
const selectedTransform = ref('spatial');
const transformCapability = ref(null);
const config = ref({
  geometry_fields: [],
  source_format: 'wkb',
  target_format: 'wkt'
});
const testResult = ref(null);

// 获取可用转换器列表
onMounted(async () => {
  const response = await axios.get('/api/transforms');
  availableTransforms.value = response.data.transforms;

  // 获取 spatial 转换器详情
  const capResponse = await axios.get('/api/transforms/spatial');
  transformCapability.value = capResponse.data;
});

// 测试转换
async function testTransform() {
  const response = await axios.post(`/api/transforms/${selectedTransform.value}/test`, {
    config: config.value,
    sample: [
      {
        id: 1,
        name: 'Test Point',
        geom: 'POINT (120.1 30.2)'
      }
    ]
  });
  testResult.value = response.data;
}
</script>
```

---

## 数据流示例

### WKB → WKT 转换流程

```
输入数据:
{
  "id": 1,
  "name": "Beijing",
  "geom": [0x01, 0x01, 0x00, 0x00, 0x00, ...]  // WKB bytes
}

↓ SpatialTransform 处理

1. 解析 WKB → geom.Point{116.4074, 39.9042}
2. 验证几何有效性（可选）
3. 序列化为 WKT

输出数据:
{
  "id": 1,
  "name": "Beijing",
  "geom": "POINT (116.4074 39.9042)"
}
```

### WKT → GeoJSON 转换流程

```
输入数据:
{
  "id": 1,
  "route": "LINESTRING (0 0, 1 1, 2 0)"
}

↓ SpatialTransform 处理

1. 解析 WKT → geom.LineString
2. 序列化为 GeoJSON

输出数据:
{
  "id": 1,
  "route": {
    "type": "LineString",
    "coordinates": [[0, 0], [1, 1], [2, 0]]
  }
}
```

---

## 性能优化建议

### 1. 批量处理

Transfer 默认以批次方式处理数据，建议配置合理的 `batch_size`:

```json
{
  "batch_size": 1000,  // 每批处理 1000 条记录
  "config": {
    "transforms": [...]
  }
}
```

### 2. 跳过验证（生产环境）

如果源数据已经过验证，可以禁用几何验证以提升性能:

```json
{
  "type": "spatial",
  "validate_geometry": false  // 跳过验证
}
```

### 3. 使用二进制格式

WKB 和 EWKB 是二进制格式，比 WKT 和 GeoJSON 更紧凑、解析更快：

- **数据库间传输**: 优先使用 WKB
- **API 导出**: 使用 GeoJSON
- **内部存储**: 使用 WKB

---

## 常见问题

### Q1: 如何处理 NULL 值？

**A**: SpatialTransform 会自动跳过 NULL 值和缺失字段，不会报错。

```json
输入: {"id": 1, "geom": null}
输出: {"id": 1, "geom": null}  // 保持不变
```

### Q2: 支持坐标系转换吗？

**A**: 当前版本预留了 `source_srid` 和 `target_srid` 参数，但暂未实现真正的投影转换。完整的坐标系转换需要集成 PROJ 库（计划在下个版本实现）。

### Q3: 如何验证转换是否正确？

**A**: 使用 `/api/transforms/:name/test` 端点测试样本数据：

```bash
curl -X POST http://localhost:8083/api/transforms/spatial/test \
  -H "Content-Type: application/json" \
  -d '{
    "config": {...},
    "sample": [{"geom": "POINT (1 2)"}]
  }'
```

### Q4: 转换失败会怎样？

**A**: 转换失败会中断整个任务，并返回详细错误信息：

```json
{
  "success": false,
  "error": "row 42, field geom: failed to parse geometry: invalid WKT"
}
```

### Q5: 如何添加自定义转换器？

**A**: 实现 `Transform` 接口并注册：

```go
// 1. 实现 Transform 接口
type MyTransform struct { ... }

func (t *MyTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
  // 转换逻辑
}

func (t *MyTransform) Name() string {
  return "MyTransform"
}

// 2. 注册到全局注册表
func init() {
  pipeline.RegisterTransform("my_transform", NewMyTransform, pipeline.TransformCapability{
    Name: "my_transform",
    Description: "My custom transform",
    // ...
  })
}
```

---

## 测试覆盖

当前测试覆盖率: **100%** (所有核心功能已测试)

```bash
# 运行所有测试
go test -v ./pkg/pipeline/...

# 运行空间转换测试
go test -v ./pkg/pipeline/... -run TestSpatial

# 查看覆盖率
go test -cover ./pkg/pipeline/...
```

---

## 下一步计划

- [ ] P1: 实现坐标系投影转换（集成 PROJ 库）
- [ ] P1: 实现几何简化算法（Douglas-Peucker）
- [ ] P1: ImageTransform 图片转换器
- [ ] P2: VideoTransform 视频转换器
- [ ] P2: 支持用户自定义转换器插件

---

## 相关文档

- [SPATIAL_AND_EXTENSIBLE_TRANSFORMS.md](SPATIAL_AND_EXTENSIBLE_TRANSFORMS.md) - 详细设计文档
- [CLAUDE.md](../CLAUDE.md) - Transfer 模块总体架构
