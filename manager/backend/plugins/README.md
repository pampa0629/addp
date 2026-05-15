# Manager 插件系统

本目录包含 Manager 模块的数据预览插件配置。运行时只读取 `manifest.json`。

`manifest.json` 负责表达产品策略：默认启用哪些内置 provider/content handler、优先级、读取限制、禁用/覆盖和 command 扩展。Go 代码仍负责实现绑定：例如 `builtin:content-image` 对应哪个 handler、`builtin:file-table` 对应哪个 provider，这类构造逻辑不放进 JSON。

## 目录结构

```
plugins/
├── README.md                    # 本文件
└── manifest.json                # 预览 Provider 和内容处理器覆盖配置
```

## 两类插件

### 1. PreviewProvider（预览提供程序）

**位置**: `providers/` 目录
**职责**: 决定**能不能预览**某种数据源/节点类型
**处理层级**: 数据源级别（数据库表、对象 catalog、Schema 节点等）

默认启用的 PreviewProvider 写在 `manifest.json` 的 `default_providers`。需要禁用、覆盖默认 provider 或扩展 command provider 时，修改 `providers`。

**配置示例**:
```json
{
  "name": "builtin:file-catalog",
  "type": "builtin",
  "builtin": "file-catalog",
  "enabled": false
}
```

**特点**:
- 处理的是"数据源类型"而非"文件类型"
- 主链路由 Meta 标准属性确定性选择 provider，配置不参与语义路由
- `default_providers` 配置默认启用列表
- `providers` 配置禁用/覆盖内置 provider 或声明 command 扩展
- 通过 `LoadPreviewPlugins()` 加载

### 2. ObjectContentHandler（对象内容处理器）

**位置**: `content/` 目录
**职责**: 决定**怎么预览**具体的文件内容
**处理层级**: 文件级别（PDF、Excel、JSON、图片、视频等）

默认启用的内容处理器写在 `manifest.json` 的 `default_content_plugins`。需要覆盖默认策略或扩展 command 处理器时，修改 `content_plugins`。

**配置示例**:
```json
{
  "name": "builtin:content-container",
  "type": "builtin",
  "builtin": "container",
  "max_bytes": 1073741824
}
```

**特点**:
- 内置处理器的匹配规则来自 `common/format` descriptor
- `default_content_plugins` 配置默认启用列表、优先级和读取限制
- `content_plugins` 配置禁用/覆盖默认大小限制、少量处理器参数，或声明 command 扩展
- 根据 Meta 标准 format 优先选择处理器，扩展名和 MIME 只作为 format 缺失时的兜底
- 通过 `LoadObjectContentPlugins()` 加载

## 调用链

```
用户请求预览
  ↓
PreviewResolver 根据 Meta 标准属性选择代码默认注册的 Provider
  ↓
Provider 执行对应预览
  ↓
如果是对象 catalog，调用 `manager/internal/objectcontent.ObjectContentRegistry`
  ↓
ContentHandler 优先根据 Meta 标准 format 匹配，必要时再使用扩展名和 Content-Type
```

## 如何扩展

### 新增数据库支持

通用关系型表预览由内置 `builtin:database-table` 默认注册。新增数据库支持优先扩展引擎能力和共享连接能力；只有接入外部 command provider 时，才在 `manifest.json` 的 `providers` 添加配置，例如：
```json
{
  "name": "command:custom-provider",
  "type": "command",
  "command": "/opt/addp/preview/custom-provider",
  "timeout": 15
}
```

### 新增文件格式支持

多数内置格式不需要配置。只有需要覆盖默认策略或接入外部 command 时，才在 `manifest.json` 的 `content_plugins` 添加配置，例如：
```json
{
  "name": "command:content-custom",
  "type": "command",
  "command": "/opt/addp/preview/custom-preview",
  "match": {
    "formats": ["custom"]
  },
  "max_bytes": 10485760
}
```

如果要新增一个已有 Go handler 支持的内置渲染类型，把它加入 `default_content_plugins` 或 `content_plugins` 即可。如果需要一种全新的处理方式，先实现 Go handler/factory，再在 manifest 中启用或覆盖它。

## 架构演进

系统从早期的"每种数据库/格式一个独立实现"演进到现在的"通用实现 + 默认内置注册 + 轻量覆盖配置"：

- **旧方式**: `preview_provider_postgres.go`、`preview_provider_mysql.go` 等独立文件
- **新方式**: `manager/internal/preview/preview_provider_database.go` 统一实现，内置 provider 默认注册，manifest 只保留覆盖和外部扩展

这种架构避免为默认能力维护空壳配置，同时保留必要的扩展入口。

## 参考文档

- 通用数据库预览: [internal/preview/preview_provider_database.go](../internal/preview/preview_provider_database.go)
- 预览插件加载器: [internal/preview/preview_plugin_loader.go](../internal/preview/preview_plugin_loader.go)
- 内容插件加载器: [internal/objectcontent/object_content_plugin_loader.go](../internal/objectcontent/object_content_plugin_loader.go)
