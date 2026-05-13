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

  const frontendRenderer = (data = {}) =>
    (
      data.object?.content?.frontend_renderer ||
      data.object?.content?.frontendRenderer ||
      data.object?.content?.metadata?.frontend_renderer ||
      data.object?.content?.metadata?.frontendRenderer ||
      ''
    ).toString().toLowerCase()

  register({
    name: 'table',
    component,
    canHandle: (data = {}) => {
      if (frontendRenderer(data) === 'table') {
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
