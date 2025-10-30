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

  register({
    name: 'table',
    component,
    canHandle: (data = {}) => (data.mode || '').toLowerCase() === 'table',
    priority: 100
  })

  console.log('📦 Table 预览插件已注册')
})()
