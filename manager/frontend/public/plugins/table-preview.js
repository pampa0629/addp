(function () {
  const COMPONENT_KEY = 'TablePreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 table 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  register({
    name: 'table',
    component,
    canHandle: (data = {}) => {
      const mode = (data.mode || '').toLowerCase()
      const object = data.object || {}
      const nodeType = (object.node_type || '').toLowerCase()

      // 处理表数据
      if (mode === 'table') {
        return true
      }

      // 处理数据库 schema/database 节点（PostgreSQL、MySQL等）
      if (mode === 'node' && ['schema', 'database'].includes(nodeType)) {
        return true
      }

      return false
    },
    priority: 100
  })

  console.log('📦 Table 预览插件已注册')
})()
