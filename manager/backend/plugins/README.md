# Manager 插件系统

本目录包含 Manager 模块的数据预览插件配置，采用声明式插件架构。

## 目录结构

```
plugins/
├── README.md                    # 本文件
├── providers/                   # 预览提供程序配置（PreviewProvider）
│   ├── 010_relational.json     # 关系型数据库表预览
│   ├── 020_builtin_object_catalog.json  # 对象 catalog 预览
│   └── 030_builtin_schema_node.json     # Schema 节点预览
└── content/                     # 内容处理器配置（ObjectContentHandler）
    ├── README.md
    ├── 110_content_pdf.json    # PDF 文件处理器
    ├── 111_content_docx.json   # Word 文档处理器
    └── ... (共 13 个文件)
```

## 两类插件

### 1. PreviewProvider（预览提供程序）

**位置**: `providers/` 目录
**职责**: 决定**能不能预览**某种数据源/节点类型
**处理层级**: 数据源级别（数据库表、对象 catalog、Schema 节点等）

**配置示例**:
```json
{
  "name": "builtin:database-table",
  "type": "builtin",
  "builtin": "database-table",
  "description": "关系型数据库表预览"
}
```

**特点**:
- 配置简单，无需匹配规则
- 处理的是"数据源类型"而非"文件类型"
- 主链路由 Meta 标准属性确定性选择 provider，配置中的顺序不参与语义路由
- 通过 `LoadPreviewPlugins()` 加载

### 2. ObjectContentHandler（对象内容处理器）

**位置**: `content/` 目录
**职责**: 决定**怎么预览**具体的文件内容
**处理层级**: 文件级别（PDF、Excel、JSON、图片、视频等）

**配置示例**:
```json
{
  "name": "builtin:content-json",
  "type": "builtin",
  "builtin": "json",
  "priority": 60,
  "match": {
    "formats": ["json"],
    "extensions": [".json", ".geojson"],
    "content_types": ["application/json", "application/geo+json"]
  },
  "max_bytes": 524288
}
```

**特点**:
- 配置复杂，包含匹配规则（标准 format、扩展名、MIME 类型）和大小限制
- 根据 Meta 标准 format 优先选择处理器，扩展名和 MIME 只作为 format 缺失时的兜底
- 通过 `LoadObjectContentPlugins()` 加载

## 调用链

```
用户请求预览
  ↓
PreviewResolver 根据 Meta 标准属性选择 Provider（providers/ 目录）
  ↓
Provider 执行对应预览
  ↓
如果是对象 catalog，调用 `manager/internal/objectcontent.ObjectContentRegistry`（content/ 目录）
  ↓
ContentHandler 优先根据 Meta 标准 format 匹配，必要时再使用扩展名和 Content-Type
```

## 如何扩展

### 新增数据库支持

在 `providers/` 目录添加新配置，例如：
```json
{
  "name": "builtin:doris-table",
  "type": "builtin",
  "builtin": "database-table"
}
```

### 新增文件格式支持

在 `content/` 目录添加新配置，例如：
```json
{
  "name": "builtin:content-parquet",
  "type": "builtin",
  "builtin": "parquet",
  "priority": 60,
  "match": {
    "formats": ["parquet"],
    "extensions": [".parquet"],
    "content_types": ["application/x-parquet"]
  },
  "max_bytes": 10485760
}
```

## 架构演进

系统从早期的"每种数据库/格式一个独立实现"演进到现在的"通用实现 + 声明式配置 + 工厂模式"：

- **旧方式**: `preview_provider_postgres.go`、`preview_provider_mysql.go` 等独立文件
- **新方式**: `manager/internal/preview/preview_provider_database.go` 统一实现 + JSON 配置

这种架构提供了更好的可扩展性和维护性。

## 参考文档

- 通用数据库预览: [internal/preview/preview_provider_database.go](../internal/preview/preview_provider_database.go)
- 预览插件加载器: [internal/preview/preview_plugin_loader.go](../internal/preview/preview_plugin_loader.go)
- 内容插件加载器: [internal/objectcontent/object_content_plugin_loader.go](../internal/objectcontent/object_content_plugin_loader.go)
