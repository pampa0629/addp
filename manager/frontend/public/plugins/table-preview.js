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
      const attrs = object.attributes || {}
      const fileType = String(attrs.file_type || '').toLowerCase()
      const kind = String(object?.content?.kind || '').toLowerCase()
      const hasTabularData = Array.isArray(data.rows) && data.rows.length > 0
      const hasGeometryColumns = Array.isArray(data.geometry_columns) && data.geometry_columns.length > 0

      if (mode === 'table') {
        return true
      }

      const isShapefile =
        fileType.includes('shp') ||
        fileType.includes('shapefile') ||
        kind === 'shapefile'

      return isShapefile && hasTabularData && hasGeometryColumns
    },
    priority: 100
  })

  console.log('📦 Table 预览插件已注册')
})()
