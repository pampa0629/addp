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

  const contentKind = (data = {}) =>
    (
      data.object?.content?.kind ||
      data.object?.content?.Kind ||
      data.object?.kind ||
      data.object?.Kind ||
      ''
    ).toString().toLowerCase()

  const contentTypeCandidates = (object = {}) => [
    object.content_type,
    object.contentType,
    object.content?.content_type,
    object.content?.contentType,
    object.content?.metadata?.content_type,
    object.content?.metadata?.contentType
  ]

  const matchesContentType = (type) => {
    if (!type) return false
    const lower = type.toLowerCase()
    return (
      lower.includes('wordprocessingml') ||
      lower === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
    )
  }

  register({
    name: 'docx',
    component,
    canHandle: (data = {}) => {
      if (frontendRenderer(data) === 'docx') {
        return true
      }
      if (contentKind(data) === 'docx') {
        return true
      }
      const object = data.object || {}
      const path = (object.path || '').toLowerCase()
      if (path.endsWith('.docx')) {
        return true
      }
      return contentTypeCandidates(object).some(matchesContentType)
    },
    priority: 64
  })

  console.log('📦 DOCX 预览插件已注册')
})()
