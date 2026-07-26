// 注意：预览组件（MarkdownPreview、DocxPreview 等）包含重型依赖（marked、mammoth、jszip 等），
// 已移至 ./previews 单独入口，避免不需要预览的模块引入这些依赖。
// 如需预览组件，请从 '@common-ui/previews' 导入。

// Basic UI Components (no map dependencies)
export { default as StorageEngineForm } from './components/StorageEngineForm.vue'
export { default as ResourceTree } from './components/ResourceTree.vue'
export { default as ResourceTreePicker } from './components/ResourceTreePicker.vue'
export { default as AuthLoginFlow } from './components/AuthLoginFlow.vue'

// Schedule Components
export { default as ScheduleConfig } from './components/ScheduleConfig.vue'
export { default as ScheduleDisplay } from './components/ScheduleDisplay.vue'

// Utils
export { formatBytes, formatDate, safeStringify } from './utils/formatters'
export * from './utils/schedule'
export * from './utils/index'
export * from './utils/engineDisplay'
export * from './utils/consoleBridge'
export * from './utils/taskOwnerUrl'
export * from './utils/continuousExecution'
export { toAmisResponse, toAmisListResponse, createAmisInterceptor } from './utils/amis-adaptor'

// Types
export * from './types/index'
export * from './types/tree'
// resourceLocator 已经通过 types/index 导出，无需重复

// Resource capability APIs
export { detectTableMetadata } from './api/resourceCapability'

// Resource Tree API
export {
  getResourceTree,
  getResourceTreeAncestors,
  getResourceTreeNode,
  listResourceTreeEngines,
  refreshResourceTreeNode,
  searchResourceTree,
  selectionFromResourceTreeNode
} from './api/resourceTree'

// Composables - Authentication
export {
  createAuthGuard,
  createAuthInterceptor,
  createAuthenticatedFetch,
  createRefreshInterceptor,
  createAuthAPI,
  createAPIClient,
  createAuthStore
} from './composables/useAuth'
export {
  createIframeAuthCoordinator,
  getAccessToken,
  getAccessTokenExpiresAt,
  setRuntimeAccessToken,
  clearRuntimeAccessToken,
  subscribeAccessToken
} from './auth/authSession'

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
