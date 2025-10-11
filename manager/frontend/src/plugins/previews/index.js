/**
 * 预览插件注册中心
 *
 * 使用方式:
 * 1. 通过 registerPreview() 注册预览插件
 * 2. 通过 getPreviewComponent() 根据数据自动选择合适的预览组件
 */

const previewRegistry = new Map()

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

  previewRegistry.set(config.name, {
    component: config.component,
    canHandle: config.canHandle,
    priority: config.priority || 0
  })

  console.log(`✅ 注册预览插件: ${config.name} (优先级: ${config.priority || 0})`)
}

/**
 * 根据数据自动选择合适的预览组件
 * @param {Object} data - 预览数据
 * @returns {Component|null} Vue 组件或 null
 */
export function getPreviewComponent(data) {
  const handlers = Array.from(previewRegistry.values())
    .filter(h => h.canHandle(data))
    .sort((a, b) => b.priority - a.priority)

  const selected = handlers[0]

  if (selected) {
    const pluginName = Array.from(previewRegistry.entries())
      .find(([, value]) => value === selected)?.[0]
    console.log(`🔍 选择预览插件: ${pluginName}`)
  } else {
    console.warn('⚠️  未找到匹配的预览插件', data)
  }

  return selected?.component || null
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

// ============= 注册内置预览插件 =============

import TablePreview from '@/components/previews/TablePreview.vue'
import ObjectStoragePreview from '@/components/previews/ObjectStoragePreview.vue'
import ImagePreview from '@/components/previews/ImagePreview.vue'
import GeoJsonPreview from '@/components/previews/GeoJsonPreview.vue'
import JsonPreview from '@/components/previews/JsonPreview.vue'
import PdfPreview from '@/components/previews/PdfPreview.vue'
import TextPreview from '@/components/previews/TextPreview.vue'

// 表格预览 (优先级最高)
registerPreview({
  name: 'table',
  component: TablePreview,
  canHandle: (data) => data.mode === 'table',
  priority: 100
})

// 对象存储预览
registerPreview({
  name: 'object-storage',
  component: ObjectStoragePreview,
  canHandle: (data) => {
    if (data.mode !== 'object') return false
    const nodeType = (data.object?.node_type || '').toLowerCase()

    // 如果是目录/前缀/bucket，则使用对象存储预览
    if (['directory', 'prefix', 'bucket'].includes(nodeType)) {
      return true
    }

    // 如果是object（文件），但没有content（内容预览），则显示对象信息
    // 有content的文件应该由具体的预览插件处理（pdf, image, json等）
    if (nodeType === 'object' && !data.object?.content) {
      return true
    }

    return false
  },
  priority: 90
})

// GeoJSON 预览
registerPreview({
  name: 'geojson',
  component: GeoJsonPreview,
  canHandle: (data) => {
    const kind = (data.object?.content?.kind || '').toLowerCase()
    return kind === 'geojson'
  },
  priority: 80
})

// 图片预览
registerPreview({
  name: 'image',
  component: ImagePreview,
  canHandle: (data) => {
    const kind = (data.object?.content?.kind || '').toLowerCase()
    return kind === 'image'
  },
  priority: 70
})

// JSON 预览
registerPreview({
  name: 'json',
  component: JsonPreview,
  canHandle: (data) => {
    const kind = (data.object?.content?.kind || '').toLowerCase()
    return kind === 'json'
  },
  priority: 60
})

// PDF 预览
registerPreview({
  name: 'pdf',
  component: PdfPreview,
  canHandle: (data) => {
    // 检查文件扩展名
    const path = (data.object?.path || '').toLowerCase()
    if (path.endsWith('.pdf')) {
      return true
    }

    // 检查 Content-Type
    const contentType = (data.object?.content_type || '').toLowerCase()
    if (contentType.includes('pdf') || contentType === 'application/pdf') {
      return true
    }

    // 检查 content kind
    const kind = (data.object?.content?.kind || '').toLowerCase()
    if (kind === 'pdf') {
      return true
    }

    return false
  },
  priority: 65
})

// 文本预览 (兜底,优先级最低)
registerPreview({
  name: 'text',
  component: TextPreview,
  canHandle: () => true, // 兜底处理所有未匹配的类型
  priority: 0
})

// ============= 用户自定义插件加载 =============

/**
 * 从全局变量加载用户自定义插件
 * 用户可以在 public/plugins/custom-preview.js 中定义:
 *
 * window.DataExplorerPlugins = window.DataExplorerPlugins || []
 * window.DataExplorerPlugins.push({
 *   name: 'csv',
 *   component: {...},
 *   canHandle: (data) => {...},
 *   priority: 50
 * })
 */
export function loadCustomPlugins() {
  if (typeof window === 'undefined') return

  const customPlugins = window.DataExplorerPlugins || []
  if (customPlugins.length === 0) {
    console.log('ℹ️  未发现自定义预览插件')
    return
  }

  console.log(`📦 加载 ${customPlugins.length} 个自定义预览插件...`)
  customPlugins.forEach((plugin) => {
    registerPreview(plugin)
  })
}

// 自动加载自定义插件
loadCustomPlugins()

export default {
  registerPreview,
  getPreviewComponent,
  getRegisteredPlugins,
  unregisterPreview,
  loadCustomPlugins
}
