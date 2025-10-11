/**
 * CSV 文件预览插件示例
 *
 * 使用方法:
 * 1. 在 index.html 中添加:
 *    <script src="/plugins/example-csv-preview.js"></script>
 *
 * 2. 重启开发服务器或刷新页面
 *
 * 3. 上传 .csv 文件到对象存储并查看预览
 */

// 确保全局插件数组存在
window.DataExplorerPlugins = window.DataExplorerPlugins || []

// 注册 CSV 预览插件
window.DataExplorerPlugins.push({
  name: 'csv-preview-example',

  // Vue 组件定义
  component: {
    template: `
      <div class="csv-preview">
        <div v-if="error" class="error-message">
          <el-alert type="error" :title="error" :closable="false" />
        </div>

        <div v-else>
          <div class="csv-toolbar">
            <span>共 {{ parsedData.length }} 行数据</span>
            <el-button size="small" @click="downloadCSV">
              <el-icon><Download /></el-icon>
              下载
            </el-button>
          </div>

          <el-table
            :data="paginatedData"
            height="400"
            border
            stripe
          >
            <el-table-column
              v-for="col in columns"
              :key="col"
              :prop="col"
              :label="col"
              show-overflow-tooltip
              min-width="120"
            />
          </el-table>

          <el-pagination
            v-if="parsedData.length > pageSize"
            background
            layout="prev, pager, next, total"
            :total="parsedData.length"
            :page-size="pageSize"
            :current-page="currentPage"
            @current-change="handlePageChange"
            style="margin-top: 12px; justify-content: center;"
          />
        </div>
      </div>
    `,

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
