# 快速上手：添加新格式支持

本文档提供添加新数据格式支持的快速指南。

---

## 5分钟快速理解架构

### 三个独立的插件系统

```
┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│  Meta Module │   │Manager Module│   │Transfer Mod. │
│              │   │              │   │              │
│  提取元数据   │   │  数据预览     │   │  读写传输     │
│  (快速扫描)   │   │  (部分加载)   │   │  (完整处理)   │
└──────────────┘   └──────────────┘   └──────────────┘
       ▲                  ▲                  ▲
       │                  │                  │
       └──────────────────┴──────────────────┘
                         │
                ┌────────▼────────┐
                │ common/format   │
                │  (共享工具)      │
                │ - 格式识别       │
                │ - 类型转换       │
                │ - Schema定义     │
                └─────────────────┘
```

### 核心概念

1. **各模块独立注册** - Meta、Manager、Transfer 各有自己的插件注册表
2. **共享工具可选** - `common/format` 提供辅助工具，但不强制使用
3. **职责分离** - 每个模块只实现自己需要的功能

---

## 添加新格式的决策树

```
开始：需要添加新格式支持（例如：Parquet）
  │
  ├─> 问题1: 哪些模块需要支持？
  │    ├─> Meta: 需要扫描文件元数据吗？ (Yes/No)
  │    ├─> Manager: 用户需要预览数据吗？ (Yes/No)
  │    └─> Transfer: 需要导入导出吗？ (Yes/No)
  │
  ├─> 问题2: common/format 需要更新吗？
  │    └─> 添加 FormatParquet 常量和MIME映射
  │
  └─> 问题3: 是否需要共享解析器？
       ├─> Yes: 在 common/format/parsers/ 创建
       └─> No: 各模块独立实现
```

---

## 实战示例：添加 Shapefile 预览支持

### 背景

- ✅ Transfer 已实现 ShapefileReader/Writer
- ✅ common/geo/shapefile 已有基础解析器
- 🔵 Manager 需要添加 Shapefile 预览功能

### Step 1: 创建预览提供者

**文件**: `manager/backend/internal/service/preview_provider_shapefile.go`

```go
package service

import (
    "context"
    "github.com/addp/common/format"
    "github.com/addp/common/geo/shapefile"
    "github.com/addp/manager/internal/models"
)

type shapefilePreviewProvider struct {
    priority int
}

func newShapefilePreviewProvider() PreviewProvider {
    return &shapefilePreviewProvider{
        priority: 90, // 高优先级
    }
}

func (p *shapefilePreviewProvider) Name() string {
    return "builtin:shapefile"
}

func (p *shapefilePreviewProvider) Priority() int {
    return p.priority
}

func (p *shapefilePreviewProvider) Supports(req *PreviewRequest) bool {
    // 检查是否为Shapefile文件
    formatType := format.DetectFormat(req.Table, nil)
    return formatType == format.FormatShapefile
}

func (p *shapefilePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
    // 使用 common/geo/shapefile 读取数据
    reader, err := shapefile.Open(req.Table)
    if err != nil {
        return nil, err
    }
    defer reader.Close()

    // 读取前100条记录
    features, err := reader.ReadAllFeatures(100)
    if err != nil {
        return nil, err
    }

    // 获取Schema
    schema := reader.GetSchema()

    // 转换为TablePreview格式
    preview := &models.TablePreview{
        Columns: convertToColumns(schema),
        Rows:    convertToRows(features),
        Total:   int64(len(features)),
    }

    return preview, nil
}

// 辅助函数
func convertToColumns(schema []shapefile.FieldInfo) []models.ColumnInfo {
    // 实现转换逻辑...
}

func convertToRows(features []shapefile.Feature) []map[string]interface{} {
    // 实现转换逻辑...
}
```

### Step 2: 注册到预览注册表

**文件**: `manager/backend/internal/service/data_explorer_service.go`

```go
func NewDataExplorerService(...) *DataExplorerService {
    previewRegistry := NewPreviewRegistry()

    // 注册现有提供者
    previewRegistry.Register(newObjectStoragePreviewProvider(...))
    previewRegistry.Register(newPostgresPreviewProvider(...))
    previewRegistry.Register(newNodePreviewProvider(...))

    // ✅ 注册新的Shapefile预览提供者
    previewRegistry.Register(newShapefilePreviewProvider())

    return &DataExplorerService{
        previewRegistry: previewRegistry,
        // ...
    }
}
```

### Step 3: 测试

```go
// manager/backend/internal/service/preview_provider_shapefile_test.go
package service

import (
    "context"
    "testing"
)

func TestShapefilePreviewProvider(t *testing.T) {
    provider := newShapefilePreviewProvider()

    req := &PreviewRequest{
        Resource: testResource,
        Table:    "testdata/sample.shp",
        Page:     1,
        PageSize: 100,
    }

    preview, err := provider.Preview(context.Background(), req)
    if err != nil {
        t.Fatalf("Preview failed: %v", err)
    }

    if len(preview.Rows) == 0 {
        t.Error("Expected preview rows")
    }
}
```

### Step 4: 更新文档

**文件**: `docs/FORMAT_SUPPORT_MATRIX.md`

```markdown
| **Shapefile** | 🔵 计划中 | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 |
```

---

## 常见场景速查

### 场景1: 只需要在Meta中扫描新格式

**步骤**：
1. 在 `meta/backend/internal/scanner/extractors/` 创建 `xxx_extractor.go`
2. 实现 `MetadataExtractor` 接口
3. 在 `extractors/init.go` 中注册: `scanner.Register(&XXXExtractor{})`

**参考**: `extractors/pdf_extractor.go`

### 场景2: 需要在所有模块支持新格式

**步骤**：
1. 在 `common/format/detection.go` 添加 `FormatXXX` 常量
2. Meta: 创建 Extractor
3. Manager: 创建 PreviewProvider
4. Transfer: 创建 Reader/Writer
5. 更新格式支持矩阵

**参考**: GeoJSON的实现（各模块都支持）

### 场景3: 复用Transfer模块的Reader实现

**步骤**：
1. Manager/Meta 直接导入 Transfer 的 connector:
   ```go
   import "github.com/addp/transfer/backend/internal/connector"
   ```
2. 使用工厂方法创建Reader:
   ```go
   reader, err := connector.NewShapefileReader(config)
   ```

**注意**: 这会引入模块间依赖，仅推荐用于临时方案

---

## 检查清单

在提交PR前，确保完成以下步骤：

- [ ] 实现了所需模块的插件（Extractor/Provider/Reader/Writer）
- [ ] 在对应的注册表中注册了插件
- [ ] 编写了单元测试
- [ ] 更新了 `docs/FORMAT_SUPPORT_MATRIX.md`
- [ ] 如果添加了新格式类型，更新了 `common/format/detection.go`
- [ ] 运行 `go test` 确保所有测试通过
- [ ] 运行 `go mod tidy` 清理依赖

---

## 常见问题

### Q: 我应该在哪个模块实现格式支持？

**A**: 根据功能需求决定：
- 只需要扫描元数据 → Meta
- 只需要预览 → Manager
- 需要完整读写 → Transfer
- 多个模块都需要 → 分别实现

### Q: 什么时候应该提取到 common/format？

**A**: 满足以下条件时考虑：
- 多个模块有相同的解析逻辑
- 解析器不依赖复杂的第三方库
- 性能要求不高（不需要极致优化）

### Q: 如何测试我的插件？

**A**: 三种测试方式：
1. 单元测试 - 测试插件的核心逻辑
2. 集成测试 - 测试插件在模块中的工作情况
3. 手动测试 - 使用实际文件验证功能

### Q: 插件的优先级如何设置？

**A**: 优先级规则：
- 100 - 高优先级（专用格式，如GeoJSON优先于通用JSON）
- 90-99 - 正常优先级（大多数格式）
- 50-89 - 低优先级（兜底处理器）
- 优先级相同时，后注册的优先

---

## 进阶阅读

- [数据格式插件化架构设计](./数据格式插件化架构设计.md) - 完整架构文档
- [格式支持矩阵](./FORMAT_SUPPORT_MATRIX.md) - 当前支持的格式列表
- [common/format API文档](../common/format/README.md) - 格式工具详细说明

---

## 获取帮助

遇到问题？
1. 查看现有实现作为参考（如 `GeoJSONExtractor`）
2. 阅读各模块的 CLAUDE.md 文档
3. 在项目Issues中提问
