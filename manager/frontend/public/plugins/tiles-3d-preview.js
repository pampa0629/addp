(function () {
  const COMPONENT_KEY = 'ThreeDTilesPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 3dtiles 注册`)
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
    const format = (
      content.format ||
      metadata.format ||
      data.object?.format ||
      ''
    ).toString().toLowerCase()
    return renderer || format
  }

  register({
    name: 'tiles-3d',
    component,
    canHandle: (data = {}) => previewTarget(data) === '3dtiles',
    priority: 89
  })

  console.log('3D Tiles 预览插件已注册')
})()
