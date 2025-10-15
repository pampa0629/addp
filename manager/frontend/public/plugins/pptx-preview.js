(function () {
  const COMPONENT_KEY = 'PptxPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 pptx 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  register({
    name: 'pptx',
    component,
    canHandle: (data = {}) => {
      const object = data.object || {}
      const path = (object.path || '').toLowerCase()
      if (path.endsWith('.pptx')) {
        return true
      }
      const contentType = (object.content_type || '').toLowerCase()
      if (
        contentType.includes('presentationml') ||
        contentType === 'application/vnd.openxmlformats-officedocument.presentationml.presentation'
      ) {
        return true
      }
      return (object.content?.kind || '').toLowerCase() === 'pptx'
    },
    priority: 63
  })

  console.log('📦 PPTX 预览插件已注册')
})()
