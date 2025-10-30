(function () {
  const COMPONENT_KEY = 'ShapefilePreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 shapefile 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  register({
    name: 'shapefile',
    component,
    canHandle: (data = {}) => (data.object?.content?.kind || '').toLowerCase() === 'shapefile',
    priority: 75
  })

  console.log('🗺️ Shapefile 预览插件已注册')
})()
