(function () {
  const component = window.DataExplorerPluginComponents?.KeyValuePreview
  if (!component) return
  const register = window.registerDataExplorerPlugin || (plugin => (window.DataExplorerPlugins ||= []).push(plugin))
  register({ name: 'key-value', component, canHandle: data => data?.mode === 'key_value' && Boolean(data.keyspace || data.key_value), priority: 100 })
})()
