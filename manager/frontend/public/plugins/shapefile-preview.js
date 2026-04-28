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
    canHandle: (data = {}) => {
      const kind = (data.object?.content?.kind || '').toLowerCase()
      if (kind !== 'shapefile') {
        return false
      }

      const hasTabularData = Array.isArray(data.rows) && data.rows.length > 0
      const hasGeometryColumns = Array.isArray(data.geometry_columns) && data.geometry_columns.length > 0
      if (hasTabularData && hasGeometryColumns) {
        return false
      }

      return true
    },
    priority: 75
  })

  console.log('🗺️ Shapefile 预览插件已注册')
})()
