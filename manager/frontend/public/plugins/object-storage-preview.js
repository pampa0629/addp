(function () {
  const COMPONENT_KEY = 'ObjectStoragePreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 object-storage 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  register({
    name: 'object-storage',
    component,
    canHandle: (data = {}) => {
      const mode = (data.mode || '').toLowerCase()
      const object = data.object || {}
      const nodeType = (object.node_type || '').toLowerCase()

      if (mode === 'node') {
        // 只处理对象存储的容器节点（不包括 schema，schema 由 table-preview 处理）
        return ['directory', 'prefix', 'bucket'].includes(nodeType)
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

  console.log('📦 Object Storage 预览插件已注册')
})()
