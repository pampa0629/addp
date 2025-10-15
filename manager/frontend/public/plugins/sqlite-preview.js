(function () {
  const registerPlugin = (plugin) => {
    const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
    if (typeof window.registerDataExplorerPlugin === 'function') {
      window.registerDataExplorerPlugin(plugin)
    } else {
      queue.push(plugin)
    }
  }

  const normalizeNumber = (value) => {
    if (typeof value !== 'number') return value
    return new Intl.NumberFormat().format(value)
  }

  const formatBytes = (bytes) => {
    if (typeof bytes !== 'number' || Number.isNaN(bytes) || bytes < 0) {
      return '--'
    }
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let index = 0
    let value = bytes
    while (value >= 1024 && index < units.length - 1) {
      value /= 1024
      index++
    }
    return `${value.toFixed(index === 0 ? 0 : 2)} ${units[index]}`
  }

  const component = {
    name: 'SqlitePreview',
    props: ['data'],
    data() {
      return {
        activeIndex: 0
      }
    },
    computed: {
      content() {
        return this.data?.object?.content || {}
      },
      json() {
        return this.content.json || {}
      },
      summary() {
        return this.json.summary || {}
      },
      tables() {
        return Array.isArray(this.json.tables) ? this.json.tables : []
      },
      activeTable() {
        return this.tables[this.activeIndex] || null
      },
      columns() {
        if (!this.activeTable) return []
        return Array.isArray(this.activeTable.columns) ? this.activeTable.columns : []
      },
      rows() {
        if (!this.activeTable) return []
        return Array.isArray(this.activeTable.rows) ? this.activeTable.rows : []
      },
      tablesTruncated() {
        return Boolean(this.summary.tables_truncated)
      },
      rowsTruncated() {
        return Boolean(this.summary.rows_truncated)
      }
    },
    watch: {
      tables: {
        immediate: true,
        handler(newTables) {
          if (!Array.isArray(newTables) || newTables.length === 0) {
            this.activeIndex = 0
            return
          }
          if (this.activeIndex >= newTables.length) {
            this.activeIndex = 0
          }
        }
      }
    },
    methods: {
      handleTableChange(value) {
        if (typeof value === 'number') {
          this.activeIndex = value
        } else if (value && typeof value === 'string') {
          const parsed = Number.parseInt(value, 10)
          if (!Number.isNaN(parsed)) {
            this.activeIndex = parsed
          }
        }
      },
      renderSummary(runtime, h, ElAlert) {
        const items = []
        const summary = this.summary
        const sizeBytes = summary.size_bytes
        const tableCount = summary.table_count
        const sampledTables = summary.sampled_tables

        items.push(
          h('div', { class: 'sqlite-summary-item' }, [
            h('span', { class: 'label' }, '表总数'),
            h('span', { class: 'value' }, normalizeNumber(tableCount ?? this.tables.length))
          ])
        )

        items.push(
          h('div', { class: 'sqlite-summary-item' }, [
            h('span', { class: 'label' }, '已加载表'),
            h('span', { class: 'value' }, normalizeNumber(sampledTables ?? this.tables.length))
          ])
        )

        items.push(
          h('div', { class: 'sqlite-summary-item' }, [
            h('span', { class: 'label' }, '文件大小'),
            h('span', { class: 'value' }, formatBytes(sizeBytes ?? this.data?.object?.size_bytes))
          ])
        )

        items.push(
          h('div', { class: 'sqlite-summary-item' }, [
            h('span', { class: 'label' }, '单表示例上限'),
            h('span', { class: 'value' }, normalizeNumber(summary.row_limit ?? 20))
          ])
        )

        const summaryBox = h('div', { class: 'sqlite-summary' }, items)

        const warnings = []
        if (this.tablesTruncated && ElAlert) {
          warnings.push(
            h(
              ElAlert,
              {
                type: 'info',
                closable: false,
                showIcon: true,
                title: '仅加载部分表'
              },
              {
                default: () => [
                  h(
                    'p',
                    { class: 'alert-tip' },
                    `当前仅展示前 ${summary.table_limit ?? this.tables.length} 张表，可下载 SQLite 文件查看更多内容。`
                  )
                ]
              }
            )
          )
        }
        if (this.rowsTruncated && ElAlert) {
          warnings.push(
            h(
              ElAlert,
              {
                type: 'info',
                closable: false,
                showIcon: true,
                title: '示例数据有限'
              },
              {
                default: () => [
                  h(
                    'p',
                    { class: 'alert-tip' },
                    `每张表仅展示前 ${summary.row_limit ?? 20} 行。`
                  )
                ]
              }
            )
          )
        }
        if (!this.tables.length && (summary.table_count || 0) > 0 && ElAlert) {
          warnings.push(
            h(
              ElAlert,
              {
                type: 'warning',
                closable: false,
                showIcon: true,
                title: '未能加载表结构'
              },
              {
                default: () => [
                  h(
                    'p',
                    { class: 'alert-tip' },
                    '检测到数据库包含数据表，但在提取结构时出现异常，请稍后重试或下载查看。'
                  )
                ]
              }
            )
          )
        }

        return [summaryBox, ...warnings]
      },
      renderTableSelector(runtime, h, ElSelect, ElOption) {
        if (!this.tables.length) {
          return null
        }
        const options = this.tables.map((table, index) => {
          const label = table.name || `表 ${index + 1}`
          return ElOption
            ? h(ElOption, {
                key: table.name || index,
                label,
                value: index
              })
            : h('option', { key: table.name || index, value: index }, label)
        })

        if (ElSelect && ElOption) {
          return h(
            'div',
            { class: 'sqlite-selector' },
            [
              h('span', { class: 'selector-label' }, '选择数据表'),
              h(
                ElSelect,
                {
                  modelValue: this.activeIndex,
                  'onUpdate:modelValue': this.handleTableChange,
                  size: 'small',
                  style: 'min-width: 240px;'
                },
                {
                  default: () => options
                }
              )
            ]
          )
        }

        return h(
          'label',
          { class: 'sqlite-selector sqlite-selector--fallback' },
          [
            h('span', { class: 'selector-label' }, '选择数据表'),
            h(
              'select',
              {
                value: this.activeIndex,
                onChange: (event) => this.handleTableChange(event.target.value)
              },
              options
            )
          ]
        )
      },
      renderDataTable(runtime, h, ElTable, ElTableColumn, ElTag) {
        if (!this.activeTable) {
          return h('div', { class: 'sqlite-empty' }, '无可用数据')
        }

        const table = this.activeTable
        const rows = this.rows
        const columns = this.columns

        const metaBadge =
          ElTag
            ? h(
                ElTag,
                { type: 'info', size: 'small' },
                { default: () => `共 ${table.row_count != null ? normalizeNumber(table.row_count) : '未知'} 行` }
              )
            : null

        const sampleBadge =
          ElTag && table.rows_truncated
            ? h(
                ElTag,
                { type: 'warning', size: 'small', effect: 'dark' },
                { default: () => '仅展示部分行' }
              )
            : null

        const typeBadge =
          ElTag && table.type
            ? h(
                ElTag,
                { type: table.type === 'view' ? 'success' : 'primary', size: 'small', effect: 'plain' },
                { default: () => (table.type === 'view' ? '视图' : '表') }
              )
            : null

        const header = h('div', { class: 'sqlite-table-header' }, [
          h(
            'div',
            { class: 'table-title' },
            [
              h('span', { class: 'name' }, table.name || `表 ${this.activeIndex + 1}`),
              typeBadge,
              metaBadge,
              sampleBadge
            ].filter(Boolean)
          )
        ])

        if (ElTable && ElTableColumn) {
          const columnNodes = columns.map((col) =>
            h(ElTableColumn, {
              key: col,
              prop: col,
              label: col,
              showOverflowTooltip: true,
              minWidth: 140
            })
          )

        return h('div', { class: 'sqlite-table' }, [
          header,
          h(
            ElTable,
            {
              data: rows,
              border: true,
              height: 420,
              size: 'small',
              stripe: true
            },
            {
              default: () => columnNodes
            }
          )
        ])
        }

        // Fallback 渲染
        return h('div', { class: 'sqlite-table sqlite-table--fallback' }, [
          header,
          h(
            'table',
            { class: 'fallback-table' },
            [
              h(
                'thead',
                null,
                h(
                  'tr',
                  null,
                  columns.map((col) => h('th', { key: col }, col))
                )
              ),
              h(
                'tbody',
                null,
                rows.map((row, idx) =>
                  h(
                    'tr',
                    { key: idx },
                    columns.map((col) => h('td', { key: col }, String(row[col] ?? '')))
                  )
                )
              )
            ]
          )
        ])
      }
    },
    render() {
      const runtime = window.Vue || {}
      const h = runtime.h
      const resolveComponent = runtime.resolveComponent

      if (typeof h !== 'function' || typeof resolveComponent !== 'function') {
        console.warn('SQLite 预览插件: Vue runtime helper 未注入')
        return null
      }

      const ElAlert = resolveComponent('ElAlert')
      const ElSelect = resolveComponent('ElSelect')
      const ElOption = resolveComponent('ElOption')
      const ElEmpty = resolveComponent('ElEmpty')
      const ElTable = resolveComponent('ElTable')
      const ElTableColumn = resolveComponent('ElTableColumn')

      if (!this.tables.length) {
        if (ElEmpty) {
          return h(ElEmpty, {
            description: 'SQLite 文件中未检测到可展示的数据表'
          })
        }
        return h('div', { class: 'sqlite-empty' }, 'SQLite 文件中未检测到可展示的数据表')
      }

      const children = [
        ...this.renderSummary(runtime, h, ElAlert),
        this.renderTableSelector(runtime, h, ElSelect, ElOption),
        this.renderDataTable(runtime, h, ElTable, ElTableColumn, resolveComponent('ElTag'))
      ].filter(Boolean)

      return h('div', { class: 'sqlite-preview' }, children)
    }
  }

  registerPlugin({
    name: 'sqlite',
    component,
    canHandle: (data = {}) => {
      const object = data.object || {}
      const content = object.content || {}
      const kind = (content.kind || '').toLowerCase()
      if (kind === 'sqlite') {
        return true
      }

      const path = (object.path || '').toLowerCase()
      if (path && (path.endsWith('.sqlite') || path.endsWith('.sqlite3') || path.endsWith('.db'))) {
        return true
      }

      const contentType = (object.content_type || '').toLowerCase()
      return contentType.includes('sqlite')
    },
    priority: 58
  })

  console.log('📦 SQLite 预览插件已注册')
})()
