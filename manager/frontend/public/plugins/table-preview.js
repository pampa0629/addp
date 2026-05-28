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

  const previewTarget = (data = {}) => {
    const content = data.object?.content || {}
    const metadata = content.metadata || {}
    const renderer = (
      content.frontend_renderer ||
      content.frontendRenderer ||
      metadata.frontend_renderer ||
      metadata.frontendRenderer ||
      ''
    ).toString().toLowerCase()
    const material = (
      content.preview_material ||
      content.previewMaterial ||
      metadata.preview_material ||
      metadata.previewMaterial ||
      ''
    ).toString().toLowerCase()
    const kind = (content.kind || content.Kind || '').toString().toLowerCase()
    return renderer || (material === 'url' || material === 'raw_binary' ? '' : material) || kind
  }

  register({
    name: 'table',
    component,
    canHandle: (data = {}) => {
      if (previewTarget(data) === 'table') {
        return true
      }
      const mode = (data.mode || '').toLowerCase()

      if (mode === 'table') {
        return true
      }

      return false
    },
    priority: 100
  })

  console.log('📦 Table 预览插件已注册')
})()
