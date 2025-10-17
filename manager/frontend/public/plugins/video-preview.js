(function () {
  const COMPONENT_KEY = 'VideoPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 video 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  register({
    name: 'video',
    component,
    canHandle: (data = {}) => {
      const object = data.object || {}
      const content = object.content || {}
      const kind = (content.kind || '').toLowerCase()
      if (kind === 'video') {
        return true
      }

      // 检查文件扩展名
      const path = (object.path || '').toLowerCase()
      if (path) {
        const videoExtensions = ['.mp4', '.avi', '.mkv', '.mov', '.webm', '.flv', '.wmv', '.m4v', '.mpg', '.mpeg']
        if (videoExtensions.some((ext) => path.endsWith(ext))) {
          return true
        }
      }

      // 检查 content_type
      const contentType = (object.content_type || '').toLowerCase()
      if (contentType.includes('video')) {
        return true
      }

      // 检查是否有 video_metadata
      const attributes = object.attributes || {}
      if (attributes.video_metadata) {
        return true
      }
      const extracted = object.extracted_metadata || attributes.extracted_metadata
      if (
        extracted &&
        extracted.custom_attrs &&
        extracted.custom_attrs.video_metadata
      ) {
        return true
      }

      return false
    },
    priority: 75
  })

  console.log('📦 Video 预览插件已注册')
})()
