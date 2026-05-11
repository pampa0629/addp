(function () {
  const COMPONENT_KEY = 'PdfPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 pdf 注册`)
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

  register({
    name: 'pdf',
    component,
    canHandle: (data = {}) => {
      if (frontendRenderer(data) === 'pdf') {
        return true
      }
      const object = data.object || {}
      const path = (object.path || '').toLowerCase()
      if (path.endsWith('.pdf')) {
        return true
      }
      const contentType = (object.content_type || '').toLowerCase()
      if (contentType.includes('pdf') || contentType === 'application/pdf') {
        return true
      }
      return (object.content?.kind || '').toLowerCase() === 'pdf'
    },
    priority: 65
  })

  console.log('📦 PDF 预览插件已注册')
})()
