(function () {
  const COMPONENT_KEY = 'ObjectCatalogPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 object-catalog 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  register({
    name: 'object-catalog',
    component,
    canHandle: (data = {}) => {
      const mode = (data.mode || '').toLowerCase()
      const object = data.object || {}
      const nodeType = (object.node_type || '').toLowerCase()

      if (mode === 'node') {
        // 处理对象 catalog容器节点和数据库结构节点（schema/database）
        return ['directory', 'prefix', 'bucket', 'schema', 'database'].includes(nodeType)
      }

      if (mode !== 'object') {
        return false
      }

      if (['directory', 'prefix', 'bucket'].includes(nodeType)) {
        return true
      }

      if (nodeType === 'object' && !object.content) {
        return true
      }

      return false
    },
    priority: 90
  })

  console.log('📦 Object Catalog 预览插件已注册')
})()
