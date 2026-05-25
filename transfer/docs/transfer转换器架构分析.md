# Transfer 架构松耦合性分析

## 核心问题

**Q**: 当前的转换器架构是否松耦合？是否易于扩展新的空间数据类型转换？

**A**: ✅ **是的！架构高度松耦合，易于扩展**

---

## 架构分析

### 1. 三层分离架构

```
┌─────────────────────────────────────────────────────────┐
│                   数据源层 (Reader/Writer)               │
│         PostgreSQL, MySQL, Shapefile, S3, MinIO...      │
└────────────────────┬────────────────────────────────────┘
                     │ 标准接口
                     ↓
┌─────────────────────────────────────────────────────────┐
│                   转换层 (Transform)                     │
│        SpatialTransform, ImageTransform, Custom...       │
└────────────────────┬────────────────────────────────────┘
                     │ 统一数据流
                     ↓
┌─────────────────────────────────────────────────────────┐
│                Pipeline Engine (引擎层)                  │
│              批处理、流式处理、错误恢复...                │
└─────────────────────────────────────────────────────────┘
```

**松耦合证明**:
- ✅ **数据源层** 与 **转换层** 完全独立
- ✅ **转换层** 只依赖统一的 `DataBatch` 结构
- ✅ **引擎层** 不关心具体的转换逻辑

---

### 2. 统一的数据交换格式

#### DataBatch - 所有组件的中间格式

```go
type DataBatch struct {
    Rows     []map[string]interface{} // 通用行数据
    Fields   []datatype.FieldInfo     // 字段事实
    Spatial  *datatype.SpatialInfo    // 空间上下文
    Metadata map[string]interface{}   // 批次元数据
    Offset   int64                    // 偏移量
}
```

**关键设计**:
- ✅ 使用 `map[string]interface{}` 表示行数据 → **完全类型无关**
- ✅ 字段事实统一使用 `datatype.FieldInfo`，不在 Transfer 内维护并行 schema 模型
- ✅ 空间数据可以是 `[]byte` (WKB)、`string` (WKT)、`map` (GeoJSON)

**松耦合证明**:
```go
// Reader 输出任意格式
batch := &DataBatch{
    Rows: []map[string]interface{}{
        {"id": 1, "geom": wkbBytes},      // PostGIS WKB
        {"id": 2, "geom": "POINT(1 2)"},  // WKT
        {"id": 3, "geom": geojsonMap},    // GeoJSON
    },
}

// Transform 处理任意格式
transform.Apply(ctx, batch)  // 不关心数据源

// Writer 接收任意格式
writer.Write(ctx, batch)     // 不关心转换逻辑
```

---

### 3. 接口驱动设计

#### Reader 接口

```go
type Reader interface {
    Open(ctx, config) error
    Read(ctx) (*DataBatch, error)
    TableInfo() *datatype.TableInfo
    Close() error
}
```

**实现示例**:
- `JDBCReader` - 关系型数据库
- `ShapefileReader` - Shapefile 文件 (待实现)
- `S3Reader` - 对象存储
- `CSVReader` - CSV 文件

**松耦合证明**:
- ✅ 每个 Reader 独立实现，互不影响
- ✅ 添加新 Reader 不需要修改现有代码
- ✅ Reader 不依赖 Transform 或 Writer

#### Transform 接口

```go
type Transform interface {
    Apply(ctx, batch) (*DataBatch, error)
    Name() string
}
```

**实现示例**:
- `SpatialTransform` - 空间数据格式转换
- `ImageTransform` - 图片处理 (待实现)
- `FieldMappingTransform` - 字段映射
- `FilterTransform` - 数据过滤

**松耦合证明**:
- ✅ Transform 只关心 `DataBatch`，不关心数据来源
- ✅ Transform 可串联（Pipeline 模式）
- ✅ 添加新 Transform 不影响现有转换器

#### Writer 接口

```go
type Writer interface {
    Open(ctx, config) error
    Write(ctx, batch) error
    Flush(ctx) error
    Close() error
}
```

**松耦合证明**:
- ✅ Writer 只接收 `DataBatch`，不关心数据来源和转换逻辑
- ✅ 添加新 Writer 不影响现有组件

---

### 4. 注册表模式 (Registry Pattern)

#### ConnectorRegistry - 数据源注册

```go
registry := NewConnectorRegistry()
registry.RegisterReader("postgresql", NewJDBCReader)
registry.RegisterReader("shapefile", NewShapefileReader)  // 新增
registry.RegisterWriter("shapefile", NewShapefileWriter)  // 新增

// 运行时动态创建
reader := registry.NewReader(ConnectorConfig{Type: "shapefile"})
```

#### TransformRegistry - 转换器注册

```go
RegisterTransform("spatial", NewSpatialTransform, capability)
RegisterTransform("image", NewImageTransform, capability)     // 新增
RegisterTransform("custom", NewCustomTransform, capability)   // 用户自定义

// 前端自动发现
transforms := ListAllTransforms()  // 自动包含新注册的转换器
```

**松耦合证明**:
- ✅ 新增组件只需注册，无需修改核心代码
- ✅ 前端通过 API 自动发现新组件
- ✅ 支持运行时动态加载（Go Plugin）

---

## 扩展性验证

### 场景 1: 添加 Shapefile 支持

**无需修改任何现有代码**:

```go
// 1. 实现 Reader
type ShapefileReader struct { ... }
func (r *ShapefileReader) Read(ctx) (*DataBatch, error) {
    // 读取 .shp，转为 DataBatch
    return &DataBatch{
        Rows: []map[string]interface{}{
            {"id": 1, "name": "...", "geom": wkbBytes},
        },
    }, nil
}

// 2. 注册
func init() {
    RegisterReader("shapefile", NewShapefileReader)
    RegisterWriter("shapefile", NewShapefileWriter)
}

// 3. 使用 (无需修改 Pipeline)
taskConfig := {
    "source": {"type": "shapefile", "path": "data.shp"},
    "target": {"type": "postgresql", "table": "cities"},
    "transforms": [{"type": "spatial", ...}]  // 复用现有转换器
}
```

### 场景 2: 添加 GeoPackage 支持

```go
// 只需实现接口和注册
type GeoPackageReader struct { ... }
func init() {
    RegisterReader("geopackage", NewGeoPackageReader)
}

// 立即可用，无需修改其他代码
```

### 场景 3: 添加自定义空间转换

```go
// 用户自定义：坐标偏移转换
type CoordinateOffsetTransform struct { offsetX, offsetY float64 }
func (t *CoordinateOffsetTransform) Apply(ctx, batch) (*DataBatch, error) {
    // 自定义转换逻辑
}

func init() {
    RegisterTransform("coordinate_offset", NewCoordinateOffsetTransform, ...)
}

// 前端自动发现，无需修改前端代码
```

---

## 松耦合的关键设计

### 1. 依赖倒置原则 (DIP)

```
高层模块 (Pipeline Engine) ← 依赖 ← 接口 (Reader/Transform/Writer)
                                   ↑
                                   实现
                                   ↑
低层模块 (JDBCReader, SpatialTransform, ShapefileWriter)
```

- ✅ Pipeline 依赖接口，不依赖具体实现
- ✅ 新增实现不影响高层逻辑

### 2. 开闭原则 (OCP)

```
对扩展开放: 添加新 Reader/Transform/Writer
对修改关闭: 无需修改现有代码
```

- ✅ 添加 ShapefileReader → 只需实现接口 + 注册
- ✅ 添加 ImageTransform → 只需实现接口 + 注册

### 3. 单一职责原则 (SRP)

```
Reader:    只负责读取数据 → DataBatch
Transform: 只负责转换 DataBatch
Writer:    只负责写入 DataBatch
Registry:  只负责管理注册
```

- ✅ 每个组件职责单一，修改不互相影响

### 4. 接口隔离原则 (ISP)

```
Reader 接口:    Read/Close (不关心 Write)
Transform 接口: Apply (不关心 Read/Write)
Writer 接口:    Write/Flush (不关心 Read)
```

- ✅ 接口最小化，减少依赖

---

## 数据流示例：PostGIS → Shapefile

```
┌─────────────────┐
│  JDBCReader     │  读取 PostGIS (输出 WKB)
│  (PostgreSQL)   │
└────────┬────────┘
         │ DataBatch{Rows: [{geom: wkbBytes}]}
         ↓
┌─────────────────┐
│SpatialTransform │  WKB → WKB (或其他格式)
│  (可选)         │
└────────┬────────┘
         │ DataBatch{Rows: [{geom: wkbBytes}]}
         ↓
┌─────────────────┐
│ShapefileWriter  │  写入 .shp 文件
└─────────────────┘
```

**松耦合证明**:
- ✅ JDBCReader 不知道后面是 Shapefile
- ✅ SpatialTransform 不知道数据来自 PostgreSQL
- ✅ ShapefileWriter 不知道数据经过了转换
- ✅ **三者完全独立，可任意组合**

---

## 实际扩展案例

### 当前支持的组合

| 源 | 转换 | 目标 | 状态 |
|----|------|------|------|
| PostgreSQL | SpatialTransform | MySQL | ✅ 可用 |
| PostgreSQL | - | PostgreSQL | ✅ 可用 |
| File (CSV) | FieldMapping | PostgreSQL | ✅ 可用 |

### 添加 Shapefile 后支持的组合

| 源 | 转换 | 目标 | 状态 |
|----|------|------|------|
| **Shapefile** | SpatialTransform | PostgreSQL | 🆕 新增 |
| PostgreSQL | SpatialTransform | **Shapefile** | 🆕 新增 |
| **Shapefile** | - | MySQL | 🆕 新增 |
| **Shapefile** | - | **Shapefile** | 🆕 新增 |
| **Shapefile** | SpatialTransform | GeoJSON | 🆕 新增 |

**松耦合证明**:
- ✅ 添加 Shapefile 后，自动支持 **5+ 种新组合**
- ✅ 无需修改 SpatialTransform（已有的转换器直接复用）
- ✅ 无需修改 JDBCReader/Writer（已有的数据源直接复用）

---

## 未来扩展路径

### 短期扩展 (容易实现)

1. **GeoPackage** (SQLite + 空间扩展)
   ```go
   type GeoPackageReader struct { *sql.DB }
   // 复用 JDBC 逻辑，只需调整 SQL 方言
   ```

2. **GeoJSON 文件**
   ```go
   type GeoJSONReader struct { file *os.File }
   // 解析 JSON，转为 DataBatch
   ```

3. **KML/KMZ**
   ```go
   type KMLReader struct { ... }
   // XML 解析 + 空间转换
   ```

### 中期扩展 (需要集成库)

4. **GeoTIFF** (栅格数据)
   ```go
   type GeoTIFFReader struct { ... }
   // 集成 GDAL 或纯 Go 库
   ```

5. **CAD (DWG/DXF)**
   ```go
   type CADReader struct { ... }
   // 集成 LibreDWG 或 ODA
   ```

### 长期扩展 (需要专业支持)

6. **ArcGIS Geodatabase**
7. **MapInfo TAB**
8. **Oracle Spatial**
9. **MongoDB GeoJSON**

**所有扩展都遵循相同模式**: 实现接口 + 注册 → 立即可用

---

## 松耦合验证清单

- [x] **接口驱动**: Reader/Transform/Writer 都是接口 ✅
- [x] **统一数据格式**: DataBatch 作为中间格式 ✅
- [x] **注册表模式**: 运行时动态发现组件 ✅
- [x] **依赖倒置**: Pipeline 依赖接口而非实现 ✅
- [x] **开闭原则**: 添加新组件无需修改现有代码 ✅
- [x] **单一职责**: 每个组件职责明确 ✅
- [x] **可组合性**: Reader/Transform/Writer 可任意组合 ✅

---

## 结论

### ✅ 架构高度松耦合

1. **数据源层** 与 **转换层** 完全解耦
2. **转换器** 之间完全独立，可串联组合
3. **新增组件** 不影响现有代码
4. **前端自动发现** 新组件，无需修改前端

### ✅ 易于扩展新空间数据类型

添加新空间数据格式（如 Shapefile、GeoPackage、KML）只需:

```go
// 1. 实现接口 (100-300 行代码)
type NewSpatialReader struct { ... }
func (r *NewSpatialReader) Read(ctx) (*DataBatch, error) { ... }

// 2. 注册 (1 行代码)
func init() { RegisterReader("new_format", NewNewSpatialReader) }

// 3. 完成！立即可用
```

### ✅ 复用现有转换器

新增的数据源**自动支持**所有已有的转换器:
- SpatialTransform (格式转换)
- FieldMappingTransform (字段映射)
- FilterTransform (数据过滤)
- ...

### 推荐：继续保持松耦合设计

在实现 Shapefile 支持时:
1. ✅ 实现独立的 ShapefileReader/Writer
2. ✅ 复用现有的 SpatialTransform
3. ✅ 不修改核心 Pipeline 代码
4. ✅ 通过注册表自动集成

---

**核心结论**: 当前架构**完全满足**松耦合要求，**非常适合**扩展多种空间数据类型！🎉
