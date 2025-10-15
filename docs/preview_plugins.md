# 数据预览插件体系

Manager 模块的前后端均引入了统一的插件注册机制，用于扩展数据预览能力而无需修改核心源码。

## 后端插件

### 注册流程

1. 设置环境变量 `PREVIEW_PLUGIN_DIR` 指向存放插件配置（`.json` 文件）的目录，可使用逗号或分号配置多个目录（系统会自动追加仓库自带的 `manager/backend/plugins` 路径）；
2. 每个配置描述一个命令型插件，启动时会被读取并注册；
3. 数据探查接口收到请求后，会按优先级选择首个满足条件的插件执行。

### 配置示例

```json
{
  "name": "csv-preview",
  "command": "/opt/addp/plugins/csv-preview",
  "args": ["--limit=100"],
  "resource_types": ["postgresql"],
  "modes": ["table"],
  "priority": 120,
  "timeout": 20
}
```

- `resource_types`：可选，限制插件服务的资源类型（全部小写）；为空时表示接受所有类型。
- `modes`：可选，支持 `table` / `object` / `node`。
- `type`：`builtin` 或 `command`，默认 `command`；当为 `builtin` 时需指定 `builtin` 字段，使用内置工厂名称。
- 插件通过 `stdin` 接收 JSON，请参考 `models.TablePreview` 结构返回结果。

请求载荷示例：

```json
{
  "schema": "public",
  "table": "orders",
  "page": 1,
  "page_size": 10,
  "mode": "table",
  "resource": {
    "id": 3,
    "name": "tenant-db",
    "resource_type": "postgresql",
    "connection_info": {
      "host": "db.internal",
      "port": "5432",
      "database": "demo",
      "username": "demo",
      "password": "******"
    }
  }
}
```

### 内置插件

仓库已经在 `manager/backend/plugins/` 目录中提供以下默认配置（对象内容插件位于子目录 `content/`）：

- `builtin:postgresql-table`：查询 PostgreSQL 表格数据；
- `builtin:object-storage`：渲染 S3 兼容对象存储文件/目录；
- `builtin:schema-node`：展示 schema / bucket 统计信息。

它们与第三方插件完全共用同一加载管道，如需调整可直接复制或修改这些 JSON 文件，也可以新增同目录下的自定义配置。第三方插件依旧可以通过提高 `priority` 覆盖官方默认行为。

### 对象内容插件

对象存储文件的预览同样通过插件扩展。系统会在 `manager/backend/plugins/` 目录中加载 `*_content_*.json` 配置，内置了以下处理器：

- `builtin:content-pdf`：读取 PDF 并返回 base64 数据；
- `builtin:content-docx`、`builtin:content-pptx`：处理 Office 文档；
- `builtin:content-image`：内联常见图片；
- `builtin:content-geojson`、`builtin:content-json`：解析 JSON/GeoJSON；
- `builtin:content-sqlite`：解析 SQLite 数据库，提取表结构与示例数据；
- `builtin:content-text`：兜底的纯文本渲染。

每个内容插件可声明匹配的扩展名或 `Content-Type`，以及执行方式：

```json
{
  "name": "custom-content-pdf",
  "type": "command",
  "command": "/opt/plugins/pdf-preview",
  "priority": 90,
  "max_bytes": 10485760,
  "match": {
    "extensions": [".pdf"],
    "content_types": ["application/pdf"]
  }
}
```

命令型插件会收到如下 payload（STDIN）：

```json
{
  "path": "reports/2024-q1.pdf",
  "extension": ".pdf",
  "content_type": "application/pdf",
  "size": 123456,
  "max_bytes": 10485760,
  "data_base64": "...",
  "truncated": false
}
```

插件需返回 `models.ObjectPreviewContent` 结构的 JSON。通过新增或覆盖内容插件，第三方即可在不修改源码的情况下自定义（或替换）PDF 等文件的处理逻辑。

## 前端插件

- 所有默认组件均以脚本形式存放在 `manager/frontend/public/plugins/` 目录（如 `table-preview.js`、`pdf-preview.js`、`sqlite-preview.js` 等），与第三方脚本位于相同目录（默认 `/plugins/manifest.json` 已包含这些脚本）；
- 应用启动时会读取 `/plugins/manifest.json`（或 `VITE_DATA_EXPLORER_PLUGIN_MANIFEST`）并按照 `scripts` 列表动态注入脚本，新增脚本只需追加到该清单即可；
- 插件脚本通过 `window.registerDataExplorerPlugin(config)` 完成注册，如脚本提前加载也可向 `window.DataExplorerPlugins` 队列推送配置。
- 官方会将 Vue 组件暴露在 `window.DataExplorerPluginComponents` 下，第三方脚本可直接复用这些组件（参见目录中的各个内置脚本）。

`config` 与后端返回的数据契约一致，需要实现：

```javascript
window.registerDataExplorerPlugin({
  name: 'csv',
  component: {
    props: ['data'],
    render() {
      const { h } = window.Vue
      return h('div', null, 'preview content')
    }
  },
  canHandle: (preview) => preview.mode === 'table',
  priority: 50
})
```

当未找到任何插件时，前端会给出明确提示，便于引导用户安装新的插件脚本。
