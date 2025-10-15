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

  register({
    name: 'image',
    component,
    canHandle: (data = {}) => {
      const object = data.object || {}
      const content = object.content || {}
      const kind = (content.kind || '').toLowerCase()
      if (kind === 'image') {
        return true
      }

      const path = (object.path || '').toLowerCase()
      if (path) {
        const imageExtensions = ['.png', '.jpg', '.jpeg', '.gif', '.bmp', '.webp', '.svg', '.tiff']
        if (imageExtensions.some((ext) => path.endsWith(ext))) {
          return true
        }
      }

      const contentType = (object.content_type || '').toLowerCase()
      if (contentType.includes('image') || contentType.includes('bmp')) {
        return true
      }

      return false
    },
    priority: 70
  })

  console.log('📦 Image 预览插件已注册')
})()
