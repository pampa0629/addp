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

  const frontendRenderer = (data = {}) =>
    (
      data.object?.content?.frontend_renderer ||
      data.object?.content?.frontendRenderer ||
      data.object?.content?.metadata?.frontend_renderer ||
      data.object?.content?.metadata?.frontendRenderer ||
      ''
    ).toString().toLowerCase()

  const previewMaterial = (data = {}) =>
    (
      data.object?.content?.preview_material ||
      data.object?.content?.previewMaterial ||
      data.object?.content?.metadata?.preview_material ||
      data.object?.content?.metadata?.previewMaterial ||
      ''
    ).toString().toLowerCase()

  register({
    name: 'geojson',
    component,
    canHandle: (data = {}) => {
      const kind = (data.object?.content?.kind || '').toLowerCase()
      if (kind === 'geojson') {
        return true
      }
      return frontendRenderer(data) === 'map' && previewMaterial(data) === 'geojson'
    },
    priority: 80
  })

  console.log('📦 GeoJSON 预览插件已注册')
})()
