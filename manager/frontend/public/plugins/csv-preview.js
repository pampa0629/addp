/**
 * CSV 文件预览插件
 *
 * 使用方法:
 * 1. 在 index.html 中添加:
 *    <script src="/plugins/csv-preview.js"></script>
 *
 * 2. 重启开发服务器或刷新页面
 *
 * 3. 上传 .csv 文件到对象存储并查看预览
 */

// 确保全局插件数组存在
window.DataExplorerPlugins = window.DataExplorerPlugins || []

const registerCsvPlugin = (pluginConfig) => {
  if (typeof window.registerDataExplorerPlugin === 'function') {
    window.registerDataExplorerPlugin(pluginConfig)
  } else {
    window.DataExplorerPlugins.push(pluginConfig)
  }
}

registerCsvPlugin({
  name: 'csv-preview',
  component: {
    name: 'CsvPreview',
    props: ['data'],

    data() {
      return {
        parsedData: [],
        columns: [],
        error: null,
        currentPage: 1,
        pageSize: 20
      }
    },

    computed: {
      paginatedData() {
        const start = (this.currentPage - 1) * this.pageSize
        const end = start + this.pageSize
        return this.parsedData.slice(start, end)
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
        try {
          if (!text || !text.trim()) {
            this.error = 'CSV 文件为空'
            return
          }

          const lines = text.trim().split('\n')
          if (lines.length === 0) {
            this.error = 'CSV 文件无有效数据'
            return
          }

          // 第一行作为表头
          this.columns = this.parseCSVLine(lines[0])

          // 其余行作为数据
          this.parsedData = lines.slice(1).map(line => {
            const values = this.parseCSVLine(line)
            const row = {}
            this.columns.forEach((col, index) => {
              row[col] = values[index] || ''
            })
            return row
          })

          this.error = null
          this.currentPage = 1

          console.log(`✅ CSV解析成功: ${this.columns.length} 列, ${this.parsedData.length} 行`)
        } catch (err) {
          this.error = `CSV解析失败: ${err.message}`
          console.error('CSV解析错误:', err)
        }
      },

      // 解析CSV行 (支持引号包裹的字段)
      parseCSVLine(line) {
        const result = []
        let current = ''
        let inQuotes = false

        for (let i = 0; i < line.length; i++) {
          const char = line[i]

          if (char === '"') {
            inQuotes = !inQuotes
          } else if (char === ',' && !inQuotes) {
            result.push(current.trim())
            current = ''
          } else {
            current += char
          }
        }

        result.push(current.trim())
        return result
      },

      handlePageChange(page) {
        this.currentPage = page
      },

      downloadCSV() {
        const text = this.data?.object?.content?.text || ''
        const filename = this.data?.object?.path?.split('/').pop() || 'data.csv'

        const blob = new Blob([text], { type: 'text/csv;charset=utf-8;' })
        const link = document.createElement('a')
        link.href = URL.createObjectURL(blob)
        link.download = filename
        link.click()
        URL.revokeObjectURL(link.href)
      }
    },

    render() {
      const runtime = window.Vue || {}
      const h = runtime.h
      const resolveComponent = runtime.resolveComponent

      if (typeof h !== 'function' || typeof resolveComponent !== 'function') {
        console.warn('DataExplorer: Vue 运行时 helper 未注入，CSV 预览无法渲染')
        return null
      }

      const ElAlert = resolveComponent('ElAlert')
      const ElButton = resolveComponent('ElButton')
      const ElIcon = resolveComponent('ElIcon')
      const DownloadIcon = resolveComponent('Download')
      const ElTable = resolveComponent('ElTable')
      const ElTableColumn = resolveComponent('ElTableColumn')
      const ElPagination = resolveComponent('ElPagination')

      if (this.error) {
        return h('div', { class: 'csv-preview' }, [
          h('div', { class: 'error-message' }, [
            ElAlert ? h(ElAlert, { type: 'error', title: this.error, closable: false }) : h('div', this.error)
          ])
        ])
      }

      const toolbar = h('div', { class: 'csv-toolbar' }, [
        h('span', null, `共 ${this.parsedData.length} 行数据`),
        ElButton
          ? h(
              ElButton,
              { size: 'small', onClick: this.downloadCSV },
              {
                default: () => [
                  ElIcon && DownloadIcon
                    ? h(ElIcon, null, { default: () => h(DownloadIcon) })
                    : null,
                  h('span', { style: 'margin-left: 4px;' }, '下载')
                ].filter(Boolean)
              }
            )
          : null
      ])

      const columns = (ElTableColumn ? this.columns : []).map((col) =>
        h(ElTableColumn, {
          key: col,
          prop: col,
          label: col,
          'show-overflow-tooltip': true,
          'min-width': 120
        })
      )

      const table = ElTable
        ? h(
            ElTable,
            {
              data: this.paginatedData,
              height: 400,
              border: true,
              stripe: true
            },
            {
              default: () => columns
            }
          )
        : h(
            'pre',
            { class: 'csv-raw' },
            (this.data?.object?.content?.text || '').trim() || '(CSV 内容为空)'
          )

      const pagination =
        ElPagination && this.parsedData.length > this.pageSize
          ? h(ElPagination, {
              background: true,
              layout: 'prev, pager, next, total',
              total: this.parsedData.length,
              'page-size': this.pageSize,
              'current-page': this.currentPage,
              onCurrentChange: this.handlePageChange,
              style: 'margin-top: 12px; justify-content: center;'
            })
          : null

      return h(
        'div',
        { class: 'csv-preview' },
        [toolbar, table, pagination].filter(Boolean)
      )
    }
  },

  // 判断是否能处理该数据
  canHandle: (data) => {
    // 检查文件路径是否以 .csv 结尾
    const path = data.object?.path || ''
    if (path.toLowerCase().endsWith('.csv')) {
      return true
    }

    // 检查 Content-Type
    const contentType = data.object?.content_type || ''
    if (contentType.includes('csv') || contentType.includes('comma-separated')) {
      return true
    }

    return false
  },

  // 优先级 (数字越大优先级越高)
  // 内置插件优先级: text(0), json(60), image(70), geojson(80), object-storage(90), table(100)
  priority: 50
})

console.log('📦 CSV 预览插件已加载')
