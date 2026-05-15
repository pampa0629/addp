# 自定义预览插件开发指南

本目录用于存放用户自定义的预览插件。官方内置的预览实现同样拆分为多个脚本 (`table-preview.js`、`container-preview.js`、`object-catalog-preview.js`、`map-preview.js`、`image-preview.js`、`json-preview.js`、`pdf-preview.js`、`docx-preview.js`、`wps-preview.js`、`pptx-preview.js`、`text-preview.js`)，方便第三方直接阅读与扩展。

## 快速开始

### 运行时加载自定义/内置插件

1. 在构建产物所在的静态目录（默认 `/plugins/`）下创建 `manifest.json`，项目已默认提供以下示例（列出了所有内置插件脚本）：

```json
{
  "scripts": [
    "/plugins/table-preview.js",
    "/plugins/container-preview.js",
    "/plugins/object-catalog-preview.js",
    "/plugins/map-preview.js",
    "/plugins/image-preview.js",
    "/plugins/json-preview.js",
    "/plugins/pdf-preview.js",
    "/plugins/docx-preview.js",
    "/plugins/pptx-preview.js",
    "/plugins/text-preview.js"
  ]
}
```

2. 将插件脚本放置在 `public/plugins/` 或部署目录能够访问的位置。

3. 启动应用后，系统会自动读取清单并注入脚本。脚本内部可调用:

```javascript
window.registerDataExplorerPlugin({
  name: 'csv',
  component: { /* Vue 组件 */ },
  canHandle: (data) => true,
  priority: 50
})
```

> 内置脚本均通过 `window.registerDataExplorerPlugin` 注册；仍可使用 `window.DataExplorerPlugins.push()` 进行兼容性注册，平台会在脚本加载完成后自动消费并注册。

插件脚本可直接使用平台暴露的 Vue 组件：

```javascript
const { TablePreview, TextPreview } = window.DataExplorerPluginComponents
```

这些组件的完整用法可参照本目录下的各个内置脚本。

> 如需覆盖内置实现（例如替换 PDF 预览逻辑），只需注册同名或更高优先级的插件脚本即可；在 `manifest.json` 中调整加载顺序，或通过 `VITE_DATA_EXPLORER_PLUGIN_MANIFEST` 指向自定义清单，实现免源码扩展。

如果需要自定义清单路径，可在运行时设置 `window.__DATA_EXPLORER_PLUGIN_MANIFEST__`，或在构建阶段配置环境变量 `VITE_DATA_EXPLORER_PLUGIN_MANIFEST`。

### 示例 1: Markdown 预览

```javascript
window.DataExplorerPlugins = window.DataExplorerPlugins || []

const ensureVueHelpers = () => {
  const runtime = window.Vue || {}
  if (typeof runtime.h !== 'function') {
    console.warn('Vue runtime helpers 未注入，Markdown 预览将无法渲染')
  }
  return runtime
}

window.DataExplorerPlugins.push({
  name: 'markdown-preview',
  component: {
    name: 'MarkdownPreview',
    props: ['data'],
    computed: {
      html() {
        const text = this.data?.object?.content?.text || ''
        if (window.marked) {
          return window.marked.parse(text)
        }
        return text.replace(/[&<>]/g, (char) => ({
          '&': '&amp;',
          '<': '&lt;',
          '>': '&gt;'
        }[char] || char)).replace(/\n/g, '<br />')
      }
    },
    render() {
      const { h } = ensureVueHelpers()
      if (typeof h !== 'function') return null
      return h('div', { class: 'markdown-preview' }, [
        h('div', { class: 'markdown-body', innerHTML: this.html })
      ])
    }
  },
  canHandle: (data) => {
    return data.object?.content?.frontend_renderer === 'markdown'
  },
  priority: 55
})
```

需要额外加载 `marked` 才能渲染 Markdown:

```html
<script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
<script src="/plugins/markdown-preview.js"></script>
```

### 示例 3: PDF 预览

```javascript
window.DataExplorerPlugins = window.DataExplorerPlugins || []

window.DataExplorerPlugins.push({
  name: 'pdf-preview',
  component: {
    name: 'PdfPreviewSimple',
    props: ['data'],
    computed: {
      pdfUrl() {
        const object = this.data?.object || {}
        return object.download_url || object.preview_url || object.url || ''
      }
    },
    render() {
      const { h } = window.Vue || {}
      if (typeof h !== 'function') return null
      const url = this.pdfUrl
      if (!url) {
        return h('div', { class: 'pdf-preview-empty' }, '无法获取 PDF 地址')
      }
      return h('div', { class: 'pdf-preview' }, [
        h('iframe', {
          src: url,
          style: 'width: 100%; height: 600px; border: none;',
          title: 'PDF 预览'
        })
      ])
    }
  },
  canHandle: (data) => {
    return data.object?.content?.frontend_renderer === 'pdf'
  },
  priority: 60
})
```

## 插件配置说明

### name (必填)
插件的唯一标识符

### component (必填)
Vue 组件定义,可以使用以下格式:

1. **内联组件** (推荐配合 `render` 函数)
```javascript
component: {
  props: ['data'],
  render() {
    const { h } = window.Vue
    return h('div', null, JSON.stringify(this.data))
  }
}
```

2. **异步组件**
```javascript
component: () => import('./my-component.vue')
```

### canHandle (必填)
判断函数,接收 `data` 参数,返回 `true` 表示该插件可以处理此数据

```javascript
canHandle: (data) => {
  // data 结构:
  // {
  //   mode: 'table' | 'object',
  //   object: {
  //     node_type: 'object' | 'directory' | 'bucket',
  //     path: '/path/to/file',
  //     content_type: 'text/plain',
  //     content: {
  //       kind: 'text' | 'json' | 'image',
  //       preview_material: 'text' | 'json' | 'geojson' | 'raw_binary' | 'url',
  //       frontend_renderer: 'text' | 'json' | 'map' | 'image' | ...
  //       text: '...',
  //       json: {...},
  //       image_data: 'base64...'
  //     }
  //   }
  // }
  return data.object?.content?.frontend_renderer === 'table'
}
```

### priority (可选)
优先级,数字越大优先级越高,默认为 0

多个插件都能处理同一数据时,选择优先级最高的插件

## 数据结构

### 表格模式 (mode: 'table')

```javascript
{
  mode: 'table',
  columns: ['id', 'name', 'geom'],
  rows: [
    { id: 1, name: 'A', geom: '{"type":"Point","coordinates":[...]}' }
  ],
  total: 100,
  geometry_columns: ['geom'],
  resourceId: 1,
  schema: 'public',
  table: 'cities'
}
```

### 对象 catalog模式 (mode: 'object')

```javascript
{
  mode: 'object',
  object: {
    node_type: 'object',  // 'object' | 'directory' | 'prefix' | 'bucket'
    bucket: 'my-bucket',
    path: 'folder/file.txt',
    size_bytes: 1024,
    content_type: 'text/plain',
    last_modified: '2025-01-01T00:00:00Z',
    metadata: { 'x-custom': 'value' },
    content: {
      kind: 'text',  // 内容类别，不等同于文件格式
      preview_material: 'text',
      frontend_renderer: 'text',
      text: 'file content...',
      truncated: false
    },
    children: [  // 仅 directory/prefix 类型有此字段
      { name: 'subfile.txt', type: 'object', size_bytes: 512, ... }
    ]
  }
}
```

## 调试技巧

1. **查看已注册插件**
```javascript
// 在浏览器控制台执行
import('@/plugins/previews').then(m => {
  console.log(m.getRegisteredPlugins())
})
```

2. **测试 canHandle 函数**
```javascript
const testData = {
  mode: 'object',
  object: {
    path: 'test.txt',
    content_type: 'text/plain'
  }
}

const plugin = window.DataExplorerPlugins[0]
console.log(plugin.canHandle(testData))  // 应返回 true/false
```

3. **查看控制台日志**

插件系统会在控制台输出调试信息:
- `✅ 注册预览插件: xxx (优先级: 50)`
- `🔍 选择预览插件: xxx`
- `⚠️  未找到匹配的预览插件`

## 完整示例项目结构

```
manager/frontend/
├── public/
│   └── plugins/
│       ├── README.md                # 本文件
│       ├── table-preview.js         # 内置表格预览
│       ├── container-preview.js     # 容器 children 预览
│       ├── object-catalog-preview.js# 对象 catalog 树/目录
│       ├── map-preview.js           # 地图预览（GeoJSON 作为预览材料）
│       ├── image-preview.js         # 图片预览（含 BMP）
│       ├── json-preview.js          # JSON 预览
│       ├── pdf-preview.js           # PDF 预览
│       ├── docx-preview.js          # DOCX 预览
│       ├── wps-preview.js           # WPS 预览
│       ├── pptx-preview.js          # PPTX 预览
│       └── text-preview.js          # 文本兜底
└── index.html
    # 添加 <script src="/plugins/xxx.js"></script>
```

## 常见问题

### Q: 插件没有生效?
A: 检查:
1. `index.html` 中是否正确引入了插件文件
2. 浏览器控制台是否有错误信息
3. `canHandle` 函数是否正确返回 `true`
4. 插件优先级是否足够高(内置插件优先级: 0-100)

### Q: 如何访问 Element Plus 组件?
A: Element Plus 已全局注册,可通过 `window.Vue.resolveComponent` 使用:
```javascript
const { h, resolveComponent } = window.Vue
const ElTable = resolveComponent('ElTable')
const ElTableColumn = resolveComponent('ElTableColumn')
return h(
  ElTable,
  { data: rows, border: true },
  {
    default: () =>
      columns.map(col =>
        h(ElTableColumn, { key: col, prop: col, label: col })
      )
  }
)
```

### Q: 如何使用外部 npm 包?
A: 通过 CDN 引入:
```html
<!-- index.html -->
<script src="https://cdn.jsdelivr.net/npm/marked@latest/marked.min.js"></script>
```

然后在插件中使用 `window.marked`

## 高级用法

### 使用 Vue 3 Composition API

```javascript
window.DataExplorerPlugins = window.DataExplorerPlugins || []

window.DataExplorerPlugins.push({
  name: 'advanced',
  component: {
    props: ['data'],
    setup() {
      const { ref } = window.Vue
      const message = ref('Hello')
      const count = ref(0)
      const handleClick = () => {
        count.value += 1
        message.value = `Clicked ${count.value} times`
      }
      return {
        message,
        handleClick,
        count
      }
    },
    render() {
      const { h, resolveComponent } = window.Vue
      const ElButton = resolveComponent?.('ElButton')
      return h('div', { class: 'advanced-preview' }, [
        h('p', null, this.message),
        ElButton
          ? h(
              ElButton,
              { type: 'primary', onClick: this.handleClick },
              { default: () => `点击 ${this.count}` }
            )
          : h('button', { onClick: this.handleClick }, `点击 ${this.count}`)
      ])
    }
  },
  canHandle: () => true,
  priority: 10
})
```

## 联系与支持

如有问题,请提交 Issue 到项目仓库。
