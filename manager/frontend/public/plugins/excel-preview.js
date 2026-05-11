(function () {
  const COMPONENT_KEY = 'ExcelPreview'

  const components = window.DataExplorerPluginComponents || {}
  const component = components[COMPONENT_KEY]

  if (!component) {
    console.warn(`DataExplorer: 内置预览组件 ${COMPONENT_KEY} 未注入，跳过 excel 注册`)
    return
  }

  const queue = (window.DataExplorerPlugins = window.DataExplorerPlugins || [])
  const register =
    typeof window.registerDataExplorerPlugin === 'function'
      ? window.registerDataExplorerPlugin
      : (plugin) => queue.push(plugin)

  const matchExtensions = ['.xlsx', '.xlsm', '.xls']
  const matchContentTypes = [
    'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    'application/vnd.ms-excel',
    'application/vnd.ms-excel.sheet.macroenabled.12'
  ]

  const frontendRenderer = (data = {}) =>
    (
      data.object?.content?.frontend_renderer ||
      data.object?.content?.frontendRenderer ||
      data.object?.content?.metadata?.frontend_renderer ||
      data.object?.content?.metadata?.frontendRenderer ||
      ''
    ).toString().toLowerCase()

  const canHandle = (data = {}) => {
    if (frontendRenderer(data) === 'excel') {
      return true
    }
    const object = data.object || {}
    const content = object.content || {}
    const path = String(object.path || '').toLowerCase()
    if (matchExtensions.some((ext) => path.endsWith(ext))) {
      return true
    }

    const contentType = String(object.content_type || '').toLowerCase()
    if (matchContentTypes.some((type) => contentType.includes(type))) {
      return true
    }

    const kind = String(content.kind || '').toLowerCase()
    if (kind === 'excel') {
      return true
    }

    const attrs = object.attributes || {}
    const extracted = attrs.extensions?.extraction?.extracted_metadata || {}
    const custom = extracted.custom_attrs || extracted.customAttrs || {}
    if (custom && (custom.excel_metadata || custom.document_type === 'xlsx' || custom.document_type === 'xlsm')) {
      return true
    }

    return false
  }

  register({
    name: 'excel-preview',
    component,
    canHandle,
    priority: 63
  })

  console.log('📦 Excel 预览插件已注册')
})()
