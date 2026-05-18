(function () {
  const COMPONENT_KEY = 'ContainerPreview'

  const components = window.DataExplorerPluginComponents || {}
  const ContainerPreview = components[COMPONENT_KEY]

  if (!ContainerPreview) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 container 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  const numberOrUndefined = (value) => {
    const number = Number(value)
    return Number.isFinite(number) ? number : undefined
  }

  const formatNumber = (value) => {
    if (typeof value !== 'number' || Number.isNaN(value)) return '--'
    return new Intl.NumberFormat().format(value)
  }

  const formatBytes = (bytes) => {
    if (typeof bytes !== 'number' || Number.isNaN(bytes) || bytes < 0) return '--'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let index = 0
    let value = bytes
    while (value >= 1024 && index < units.length - 1) {
      value /= 1024
      index++
    }
    return `${value.toFixed(index === 0 ? 0 : 2)} ${units[index]}`
  }

  const normalizeChild = (child, index) => {
    const key = child?.key || child?.name || child?.table || String(index)
    const columns = Array.isArray(child?.columns)
      ? child.columns
      : Array.isArray(child?.headers)
        ? child.headers
        : []
    const columnTypes = Array.isArray(child?.column_types)
      ? child.column_types
      : Array.isArray(child?.columnTypes)
        ? child.columnTypes
        : []
    return {
      key,
      name: child?.name || key,
      label: child?.label || child?.name || child?.table || key,
      kind: child?.kind || 'child',
      dataType: child?.data_type || child?.dataType || 'unknown',
      format: child?.format || '',
      organization: child?.organization || '',
      rowCount: numberOrUndefined(child?.row_count ?? child?.rowCount),
      columnCount: numberOrUndefined(child?.column_count ?? child?.columnCount ?? columns.length),
      hasHeader: child?.has_header ?? child?.hasHeader,
      components: Array.isArray(child?.components) ? child.components : [],
      columns,
      columnTypes,
      rows: []
    }
  }

  const childPreviewPlugin = (data) => {
    const resolve =
      typeof window.getDataExplorerPreviewComponentExcept === 'function'
        ? window.getDataExplorerPreviewComponentExcept
        : null
    if (!resolve || !data) return null
    return resolve(data, ['container-preview'])
  }

  const component = {
    name: 'GenericContainerPreview',
    props: ['data', 'activeChildPreview', 'activeChildLoading'],
    emits: ['child-change', 'page-change'],
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
      formatName() {
        return String(this.json.format || this.content.metadata?.format || '').toLowerCase()
      },
      children() {
        const list = Array.isArray(this.json.children) ? this.json.children : []
        return list.map(normalizeChild)
      },
      defaultChildKey() {
        return this.json.active_child || this.json.default_child || this.children[0]?.key || ''
      },
      selectorLabel() {
        return '子项'
      },
      emptyText() {
        return '暂无可预览的子项'
      },
      activeChildPreviewComponent() {
        return childPreviewPlugin(this.activeChildPreview)?.component || null
      },
      summaryItems() {
        const summary = this.summary || {}
        const items = []
        const rawCount = numberOrUndefined(summary.raw_child_count ?? summary.child_count)
        const visibleCount = numberOrUndefined(summary.visible_child_count ?? summary.sampled_children)
        const filteredCount = numberOrUndefined(summary.filtered_child_count ?? summary.ignored_child_count)
        const groupedComponentCount = numberOrUndefined(summary.grouped_component_count)
        items.push({ label: '原始条目', value: formatNumber(rawCount ?? this.children.length) })
        items.push({ label: '可预览子项', value: formatNumber(visibleCount ?? this.children.length) })
        if (filteredCount !== undefined && filteredCount > 0) {
          items.push({ label: '已过滤', value: formatNumber(filteredCount) })
        }
        if (groupedComponentCount !== undefined && groupedComponentCount > 0) {
          items.push({ label: '已组合组件', value: formatNumber(groupedComponentCount) })
        }
        const sizeBytes = numberOrUndefined(summary.size_bytes ?? this.data?.object?.size_bytes)
        if (sizeBytes !== undefined) {
          items.push({ label: '文件大小', value: formatBytes(sizeBytes) })
        }
        return items
      },
      childrenTruncated() {
        return Boolean(this.summary.children_truncated)
      }
    },
    methods: {
      handleChildChange(child) {
        const name = child?.name || child?.key
        if (!name) return
        this.$emit('child-change', {
          childName: name,
          refPath: child?.refPath || '',
          nestedChildPath: child?.nestedChildPath || ''
        })
      },
      handlePageChange(page) {
        this.$emit('page-change', page)
      }
    },
    render() {
      const h = window.Vue?.h
      if (typeof h !== 'function') {
        console.warn('Container 预览插件: Vue runtime helper 未注入')
        return null
      }
      return h(ContainerPreview, {
        summaryItems: this.summaryItems,
        children: this.children,
        defaultChildKey: this.defaultChildKey,
        selectorLabel: this.selectorLabel,
        activeChildPreview: this.activeChildPreview,
        activeChildLoading: Boolean(this.activeChildLoading),
        activeChildPreviewComponent: this.activeChildPreviewComponent,
        truncated: this.childrenTruncated,
        emptyText: this.emptyText,
        onChildChange: this.handleChildChange,
        onPageChange: this.handlePageChange
      })
    }
  }

  register({
    name: 'container-preview',
    component,
    canHandle: (data = {}) => {
      const content = data.object?.content || {}
      const renderer = (
        content.frontend_renderer ||
        content.frontendRenderer ||
        content.metadata?.frontend_renderer ||
        content.metadata?.frontendRenderer ||
        ''
      ).toString().toLowerCase()
      return renderer === 'container'
    },
    priority: 64
  })

  console.log('Container 预览插件已注册')
})()
