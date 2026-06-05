// 注意：预览组件（MarkdownPreview、DocxPreview 等）包含重型依赖（marked、mammoth、jszip 等），
// 已移至 ./previews 单独入口，避免不需要预览的模块引入这些依赖。
// 如需预览组件，请从 '@common-ui/previews' 导入。

// Basic UI Components (no map dependencies)
export { default as StorageEngineForm } from './components/StorageEngineForm.vue'
export { default as EngineForm } from './components/EngineForm.vue'
export { default as ImagePreview } from './components/ImagePreview.vue'
export { default as ResourceTree } from './components/ResourceTree.vue'

// Data Source Selector Components
export { default as DataSourceSelector } from './components/DataSourceSelector.vue'
export { default as DataSourceSelectorDialog } from './components/DataSourceSelectorDialog.vue'
export { default as DataSourceSelectorCard } from './components/DataSourceSelectorCard.vue'

// Data Source Cascader Components (级联选择器)
export { default as DataSourceCascader } from './components/DataSourceCascader.vue'
export { default as DataSourceCascaderDialog } from './components/DataSourceCascaderDialog.vue'
export { default as DataSourceCascaderCard } from './components/DataSourceCascaderCard.vue'

// Schedule Components
export { default as ScheduleConfig } from './components/ScheduleConfig.vue'
export { default as ScheduleDisplay } from './components/ScheduleDisplay.vue'

// Utils
export { formatBytes, formatDate, safeStringify } from './utils/formatters'
export * from './utils/schedule'
export * from './utils/index'
export * from './utils/engineDisplay'
export * from './utils/consoleBridge'
export { toAmisResponse, toAmisListResponse, createAmisInterceptor } from './utils/amis-adaptor'

// Types
export * from './types/index'
export * from './types/tree'
// resourceLocator 已经通过 types/index 导出，无需重复

// Data Source API (显式导出，避免命名冲突)
export {
  getEngines,
  getEngineTree,
  getNodeChildren,
  detectTableMetadata,
  extractDataSourceSelection,
  parseLocator
} from './api/dataSource'

// System Catalog API
export {
  normalizeCatalogPath,
  listCatalogChildren,
  listCatalogBrowserNodes,
  browserPathToCatalogPath,
  catalogPathToString,
  catalogNodesFromResponse,
  toCatalogBrowserNode,
  catalogNodeBrowserType
} from './api/catalog'

// Composables - Authentication
export {
  createAuthGuard,
  createAuthInterceptor,
  createRefreshInterceptor,
  createAuthAPI,
  createAPIClient,
  createAuthStore
} from './composables/useAuth'

// Composables - Tree Management
export { useTreeCache, useTreeLoader } from './composables'

// Composables - Resizable
export { useResizable } from './composables/useResizable'

// Composables - Theme Management
export { useTheme } from './composables/useTheme'
export { THEME_CONFIGS, THEME_VALUES, getThemeConfig, isThemeDark } from './config/themes'

// Theme Components
export { default as ThemeSwitcher } from './components/ThemeSwitcher.vue'

// Composables - I18n
export {
  createAddpI18n,
  useAddpI18n,
  getCurrentLang,
  SUPPORTED_LANGS,
  SUPPORTED_LANGUAGES,
  DEFAULT_LANG
} from './composables/useAddpI18n'

// I18n Components
export { default as LangSwitcher } from './components/LangSwitcher.vue'
