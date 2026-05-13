(function () {
  const COMPONENT_KEY = 'ContainerPreview'

  const components = window.DataExplorerPluginComponents || {}
  const ContainerPreview = components[COMPONENT_KEY]

  if (!ContainerPreview) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 sqlite 注册`)
    return
  }

  const registerPlugin = (plugin) => {
    const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
    if (typeof window.registerDataExplorerPlugin === 'function') {
      window.registerDataExplorerPlugin(plugin)
    } else {
      queue.push(plugin)
    }
  }

  const numberOrUndefined = (value) => {
    const number = Number(value)
    return Number.isFinite(number) ? number : undefined
  }

  const numberOrDefault = (value, fallback) => {
    const number = numberOrUndefined(value)
    return number === undefined ? fallback : number
  }

  const formatNumber = (value) => {
    if (typeof value !== 'number' || Number.isNaN(value)) return '--'
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

  const normalizeColumns = (columns) => {
    if (!Array.isArray(columns)) return { names: [], types: [] }
    const names = []
    const types = []
    for (const column of columns) {
      if (typeof column === 'string') {
        names.push(column)
        types.push('string')
        continue
      }
      if (!column || typeof column !== 'object') continue
      const name = column.name || column.column_name || column.columnName || column.field || ''
      if (!name) continue
      names.push(name)
      types.push(column.type || column.data_type || column.dataType || column.original_type || 'string')
    }
    return { names, types }
  }

  const tableKey = (table, index) => {
    return table?.name || table?.table || table?.table_name || table?.tableName || String(index)
  }

  const tableLabel = (table, index) => {
    return table?.name || table?.table || table?.table_name || table?.tableName || `表 ${index + 1}`
  }

  const component = {
    name: 'SqlitePreview',
    props: ['data', 'activeSheetPreview', 'activeSheetLoading'],
    emits: ['sheet-change', 'page-change'],
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
      defaultChildKey() {
        const explicit = this.json.active_table || this.json.default_table || ''
        const target = this.tables.find((table, index) => tableKey(table, index) === explicit) || this.tables[0]
        return target ? tableKey(target, this.tables.indexOf(target)) : ''
      },
      children() {
        return this.tables.map((table, index) => {
          const columns = normalizeColumns(table.columns || table.fields)
          return {
            key: tableKey(table, index),
            name: tableKey(table, index),
            label: tableLabel(table, index),
            kind: table.type || table.kind || 'table',
            dataType: table.data_type || table.dataType || 'table',
            rowCount: numberOrUndefined(table.row_count ?? table.rowCount),
            columnCount: numberOrDefault(table.column_count ?? table.columnCount, columns.names.length),
            columns: columns.names,
            columnTypes: columns.types,
            rows: Array.isArray(table.rows) ? table.rows : []
          }
        })
      },
      summaryItems() {
        const summary = this.summary
        const items = [
          { label: '表总数', value: formatNumber(numberOrDefault(summary.table_count, this.tables.length)) },
          { label: '已加载表', value: formatNumber(numberOrDefault(summary.sampled_tables, this.tables.length)) }
        ]
        const sizeBytes = numberOrUndefined(summary.size_bytes ?? this.data?.object?.size_bytes)
        if (sizeBytes !== undefined) {
          items.push({ label: '文件大小', value: formatBytes(sizeBytes) })
        }
        return items
      },
      childrenTruncated() {
        return Boolean(this.summary.children_truncated || this.summary.tables_truncated)
      }
    },
    methods: {
      handleChildChange(child) {
        const name = child?.name || child?.key
        if (!name) return
        this.$emit('sheet-change', name)
      },
      handlePageChange(page) {
        this.$emit('page-change', page)
      }
    },
    render() {
      const runtime = window.Vue || {}
      const h = runtime.h
      if (typeof h !== 'function') {
        console.warn('SQLite 预览插件: Vue runtime helper 未注入')
        return null
      }

      return h(ContainerPreview, {
        summaryItems: this.summaryItems,
        children: this.children,
        defaultChildKey: this.defaultChildKey,
        selectorLabel: '选择数据表',
        activeChildPreview: this.activeSheetPreview,
        activeChildLoading: Boolean(this.activeSheetLoading),
        truncated: this.childrenTruncated,
        emptyText: 'SQLite 文件中未检测到可展示的数据表',
        onChildChange: this.handleChildChange,
        onPageChange: this.handlePageChange
      })
    }
  }

  registerPlugin({
    name: 'sqlite',
    component,
    canHandle: (data = {}) => {
      const object = data.object || {}
      const content = object.content || {}
      const renderer = (
        content.frontend_renderer ||
        content.frontendRenderer ||
        content.metadata?.frontend_renderer ||
        content.metadata?.frontendRenderer ||
        ''
      ).toString().toLowerCase()
      if (renderer === 'sqlite') {
        return true
      }
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

  console.log('SQLite 预览插件已注册')
})()
