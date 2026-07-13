(function () {
  const component = window.DataExplorerPluginComponents?.CadPreview
  if (!component) return
  const register = window.registerDataExplorerPlugin || ((plugin) => (window.DataExplorerPlugins = window.DataExplorerPlugins || []).push(plugin))
  register({
    name: 'cad',
    component,
    canHandle: (data = {}) => {
      const content = data.object?.content || {}
      const metadata = content.metadata || {}
      return String(content.frontend_renderer || metadata.frontend_renderer || '').toLowerCase() === 'cad'
    },
    priority: 95
  })
})()
