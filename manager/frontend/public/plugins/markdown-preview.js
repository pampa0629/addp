(function () {
  const COMPONENT_KEY = 'MarkdownPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 markdown 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  const matchesContentType = (type = '') => {
    const lower = String(type).toLowerCase()
    return lower.includes('markdown')
  }

  const matchesExtension = (path = '') => {
    const lower = String(path).toLowerCase()
    return lower.endsWith('.md') || lower.endsWith('.markdown')
  }

  register({
    name: 'markdown',
    component,
    canHandle: (data = {}) => {
      const object = data.object || {}
      if (matchesExtension(object.path)) {
        return true
      }
      const extension = (object.extension || object.Extension || '').toString().toLowerCase()
      if (extension === '.md' || extension === 'md' || extension === '.markdown' || extension === 'markdown') {
        return true
      }
      const kind = (object.content?.kind || object.content?.Kind || '').toString().toLowerCase()
      if (kind === 'markdown') {
        return true
      }
      const candidates = [
        object.content_type,
        object.contentType,
        object.content?.content_type,
        object.content?.contentType,
        object.content?.metadata?.content_type,
        object.content?.metadata?.contentType
      ]
      return candidates.some(matchesContentType)
    },
    priority: 56
  })

  console.log('📦 Markdown 预览插件已注册')
})()
