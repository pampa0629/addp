(function () {
  const component = window.DataExplorerPluginComponents?.GridFilePreview
  if (!component) return
  const register = typeof window.registerDataExplorerPlugin === 'function'
    ? window.registerDataExplorerPlugin
    : plugin => (window.DataExplorerPlugins ||= []).push(plugin)
  register({ name: 'grid-file-preview', component, priority: 120,
    canHandle: data => ['netcdf', 'grib'].includes(data?.object?.attributes?.item?.format)
  })
})()
