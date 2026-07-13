(function () {
  const component = window.DataExplorerPluginComponents?.S3MPreview
  if (!component) return
  const register = window.registerDataExplorerPlugin || ((plugin) => (window.DataExplorerPlugins = window.DataExplorerPlugins || []).push(plugin))
  register({
    name: 's3m',
    component,
    canHandle: (data = {}) => {
      const content = data.object?.content || {}
      const metadata = content.metadata || {}
      return String(content.frontend_renderer || metadata.frontend_renderer || '').toLowerCase() === 's3m'
    },
    priority: 90
  })
})()
