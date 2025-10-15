/**
 * 预览插件注册中心
 *
 * 使用方式:
 * 1. 通过 registerPreview() 注册预览插件
 * 2. 通过 getPreviewComponent() 根据数据自动选择合适的预览组件
 */

const previewRegistry = new Map()

function normalizePriority(priority) {
  if (typeof priority !== 'number' || Number.isNaN(priority)) {
    return 0
  }
  return priority
}

/**
 * 注册预览插件
 * @param {Object} config - 插件配置
 * @param {string} config.name - 插件名称
 * @param {Component} config.component - Vue 组件
 * @param {Function} config.canHandle - 判断函数 (data) => boolean
 * @param {number} config.priority - 优先级(数字越大优先级越高)
 */
export function registerPreview(config) {
  if (!config.name) {
    console.error('预览插件必须有 name 属性')
    return
  }

  if (!config.component) {
    console.error(`预览插件 ${config.name} 必须有 component 属性`)
    return
  }

  if (typeof config.canHandle !== 'function') {
    console.error(`预览插件 ${config.name} 的 canHandle 必须是函数`)
    return
  }

  const entry = {
    name: config.name,
    component: config.component,
    canHandle: config.canHandle,
    priority: normalizePriority(config.priority)
  }

  previewRegistry.set(config.name, entry)

  console.log(`✅ 注册预览插件: ${config.name} (优先级: ${config.priority || 0})`)
}

/**
 * 根据数据自动选择合适的预览组件
 * @param {Object} data - 预览数据
 * @returns {Object|null} 插件信息或 null
 */
export function getPreviewComponent(data) {
  const handlers = Array.from(previewRegistry.entries())
    .map(([name, value]) => ({ name, ...value }))
    .filter(h => {
      try {
        return typeof h.canHandle === 'function' && h.canHandle(data)
      } catch (error) {
        console.error(`⚠️  预览插件 ${h.name} canHandle 抛出异常`, error)
        return false
      }
    })
    .sort((a, b) => b.priority - a.priority)

  const selected = handlers[0]

  if (selected) {
    console.log(`🔍  选择预览插件: ${selected.name} (优先级: ${selected.priority})`)
  } else {
    console.warn('⚠️  未找到匹配的预览插件', data)
  }

  return selected || null
}

/**
 * 获取所有已注册的插件名称
 */
export function getRegisteredPlugins() {
  return Array.from(previewRegistry.keys())
}

/**
 * 移除已注册的插件
 */
export function unregisterPreview(name) {
  const result = previewRegistry.delete(name)
  if (result) {
    console.log(`🗑️  移除预览插件: ${name}`)
  }
  return result
}

/**
 * 将 window.DataExplorerPlugins 队列中的插件注册到系统中
 * @returns {string[]} 已注册插件名称
 */
export function loadCustomPlugins() {
  if (typeof window === 'undefined') return []

  const queue = window.DataExplorerPlugins
  if (!Array.isArray(queue) || queue.length === 0) {
    return []
  }

  const registered = []
  while (queue.length) {
    const plugin = queue.shift()
    if (!plugin) continue
    registerPreview(plugin)
    registered.push(plugin.name || 'anonymous')
  }
  return registered
}

const initiallyLoaded = loadCustomPlugins()
if (initiallyLoaded.length) {
  console.log(`📦 自动加载 ${initiallyLoaded.length} 个自定义预览插件`)
}

if (typeof window !== 'undefined') {
  window.registerDataExplorerPlugin = (plugin) => {
    window.DataExplorerPlugins = window.DataExplorerPlugins || []
    window.DataExplorerPlugins.push(plugin)
    const registered = loadCustomPlugins()
    if (registered.length) {
      console.log(`📦 动态注册 ${registered.join(', ')}`)
    }
  }
}

export default {
  registerPreview,
  getPreviewComponent,
  getRegisteredPlugins,
  unregisterPreview,
  loadCustomPlugins
}
