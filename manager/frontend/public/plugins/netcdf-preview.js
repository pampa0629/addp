(function () {
  const component = window.DataExplorerPluginComponents?.NetCDFPreview
  if (!component) return
  const register = typeof window.registerDataExplorerPlugin === 'function'
    ? window.registerDataExplorerPlugin
    : plugin => (window.DataExplorerPlugins ||= []).push(plugin)
  register({ name: 'netcdf-preview', component, priority: 120,
    canHandle: data => data?.object?.attributes?.item?.format === 'netcdf'
  })
})()
