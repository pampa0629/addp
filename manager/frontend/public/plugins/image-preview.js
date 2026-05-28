(function () {
  const COMPONENT_KEY = 'ImagePreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 image 注册`)
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
    name: 'image',
    component,
    canHandle: (data = {}) => {
      return previewTarget(data) === 'image'
    },
    priority: 70
  })

  console.log('📦 Image 预览插件已注册')
})()
