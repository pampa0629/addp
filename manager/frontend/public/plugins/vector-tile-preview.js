(function () {
  const component = (window.DataExplorerPluginComponents || {}).VectorTilePreview
  if (!component) return
  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register = typeof window.registerDataExplorerPlugin === 'function'
    ? window.registerDataExplorerPlugin
    : (plugin) => queue.push(plugin)
  register({
    name: 'vector-tile',
    component,
    canHandle: (data = {}) => {
      const content = data.object?.content || {}
      const metadata = content.metadata || {}
      return String(content.frontend_renderer || metadata.frontend_renderer || '').trim().toLowerCase() === 'vector_tile'
    },
    priority: 95
  })
})()
