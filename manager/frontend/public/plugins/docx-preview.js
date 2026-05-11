(function () {
  const COMPONENT_KEY = 'DocxPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 docx 注册`)
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
    name: 'docx',
    component,
    canHandle: (data = {}) => {
      if (frontendRenderer(data) === 'docx') {
        return true
      }
      const object = data.object || {}
      const path = (object.path || '').toLowerCase()
      if (path.endsWith('.docx')) {
        return true
      }
      const contentType = (object.content_type || '').toLowerCase()
      if (
        contentType.includes('wordprocessingml') ||
        contentType === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
      ) {
        return true
      }
      return (object.content?.kind || '').toLowerCase() === 'docx'
    },
    priority: 64
  })

  console.log('📦 DOCX 预览插件已注册')
})()
