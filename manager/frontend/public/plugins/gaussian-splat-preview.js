(function () {
  const COMPONENT_KEY = 'GaussianSplatPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: builtin preview component ${COMPONENT_KEY} is not available, gaussian_splat plugin skipped`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  const previewTarget = (data = {}) => {
    const content = data.object?.content || {}
    const metadata = content.metadata || {}
    const renderer = (
      content.frontend_renderer ||
      content.frontendRenderer ||
      metadata.frontend_renderer ||
      metadata.frontendRenderer ||
      ''
    ).toString().toLowerCase()
    const kind = (content.kind || content.Kind || '').toString().toLowerCase()
    return renderer || kind
  }

  register({
    name: 'gaussian-splat',
    component,
    canHandle: (data = {}) => previewTarget(data) === 'gaussian_splat',
    priority: 88
  })

  console.debug('DataExplorer: gaussian_splat preview plugin registered')
})()
