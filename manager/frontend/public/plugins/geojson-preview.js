(function () {
  const COMPONENT_KEY = 'GeoJsonPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 geojson 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  register({
    name: 'geojson',
    component,
    canHandle: (data = {}) => (data.object?.content?.kind || '').toLowerCase() === 'geojson',
    priority: 80
  })

  console.log('📦 GeoJSON 预览插件已注册')
})()
