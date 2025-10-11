# 自定义预览插件开发指南

本目录用于存放用户自定义的预览插件。

## 快速开始

### 示例 1: CSV 文件预览

创建文件 `csv-preview-plugin.js`:

```javascript
// 定义插件数组
window.DataExplorerPlugins = window.DataExplorerPlugins || []

// 注册 CSV 预览插件
window.DataExplorerPlugins.push({
  name: 'csv',
  component: {
    template: `
      <div class="csv-preview">
        <el-table :data="parsedData" height="400">
          <el-table-column
            v-for="col in columns"
            :key="col"
            :prop="col"
            :label="col"
            show-overflow-tooltip
          />
        </el-table>
      </div>
    `,
    props: ['data'],
    data() {
      return {
        parsedData: [],
        columns: []
      }
    },
    watch: {
      data: {
        immediate: true,
        handler(newData) {
          this.parseCSV(newData?.object?.content?.text || '')
        }
      }
    },
    methods: {
      parseCSV(text) {
        if (!text) return
        const lines = text.trim().split('\n')
        if (lines.length === 0) return

        // 第一行作为表头
        this.columns = lines[0].split(',').map(c => c.trim())

        // 其余行作为数据
        this.parsedData = lines.slice(1).map(line => {
          const values = line.split(',')
          const row = {}
          this.columns.forEach((col, index) => {
            row[col] = values[index]?.trim() || ''
          })
          return row
        })
      }
    }
  },
  canHandle: (data) => {
    const contentType = data.object?.content_type || ''
    const path = data.object?.path || ''
    return contentType.includes('csv') || path.endsWith('.csv')
  },
  priority: 50
})
```

在 `index.html` 中引入:

```html
<script src="/plugins/csv-preview-plugin.js"></script>
```

### 示例 2: Markdown 预览

创建文件 `markdown-preview-plugin.js`:

```javascript
window.DataExplorerPlugins = window.DataExplorerPlugins || []

window.DataExplorerPlugins.push({
  name: 'markdown',
  component: {
    template: `
      <div class="markdown-preview">
        <div v-html="renderedHtml" class="markdown-body"></div>
      </div>
    `,
    props: ['data'],
    computed: {
      renderedHtml() {
        const text = this.data?.object?.content?.text || ''
        if (!window.marked) {
          return '<pre>' + text + '</pre>'
        }
        return window.marked.parse(text)
      }
    }
  },
  canHandle: (data) => {
    const path = data.object?.path || ''
    const contentType = data.object?.content_type || ''
    return path.endsWith('.md') || contentType.includes('markdown')
  },
  priority: 55
})
```

需要在 `index.html` 中引入 marked.js:

```html
<script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
<script src="/plugins/markdown-preview-plugin.js"></script>
```

### 示例 3: PDF 预览

创建文件 `pdf-preview-plugin.js`:

```javascript
window.DataExplorerPlugins = window.DataExplorerPlugins || []

window.DataExplorerPlugins.push({
  name: 'pdf',
  component: {
    template: `
      <div class="pdf-preview">
        <iframe
          :src="pdfUrl"
          style="width: 100%; height: 600px; border: none;"
        ></iframe>
      </div>
    `,
    props: ['data'],
    computed: {
      pdfUrl() {
        // 假设后端返回了 PDF 的访问 URL
        return this.data?.object?.download_url || ''
      }
    }
  },
  canHandle: (data) => {
    const contentType = data.object?.content_type || ''
    const path = data.object?.path || ''
    return contentType.includes('pdf') || path.endsWith('.pdf')
  },
  priority: 60
})
```

## 插件配置说明

### name (必填)
插件的唯一标识符

### component (必填)
Vue 组件定义,可以使用以下格式:

1. **内联组件** (如上述示例)
```javascript
component: {
  template: '...',
  props: ['data'],
  // ... 其他 Vue 组件选项
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
  //     content_type: 'text/csv',
  //     content: {
  //       kind: 'text' | 'json' | 'image' | 'geojson',
  //       text: '...',
  //       json: {...},
  //       image_data: 'base64...'
  //     }
  //   }
  // }

  const contentType = data.object?.content_type || ''
  return contentType.includes('csv')
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

### 对象存储模式 (mode: 'object')

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
      kind: 'text',  // 'text' | 'json' | 'image' | 'geojson'
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
    path: 'test.csv',
    content_type: 'text/csv'
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
│       ├── README.md              # 本文件
│       ├── csv-preview-plugin.js
│       ├── markdown-preview-plugin.js
│       └── pdf-preview-plugin.js
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
A: Element Plus 已全局注册,可以直接在 template 中使用:
```javascript
component: {
  template: `
    <el-table :data="data">
      <el-table-column prop="name" label="名称" />
    </el-table>
  `
}
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
window.DataExplorerPlugins.push({
  name: 'advanced',
  component: {
    template: `
      <div>
        <p>{{ message }}</p>
        <button @click="handleClick">点击</button>
      </div>
    `,
    props: ['data'],
    setup(props) {
      const { ref, computed } = window.Vue
      const message = ref('Hello')

      const handleClick = () => {
        message.value = 'Clicked!'
      }

      return {
        message,
        handleClick
      }
    }
  },
  canHandle: (data) => true,
  priority: 10
})
```

## 联系与支持

如有问题,请提交 Issue 到项目仓库。
