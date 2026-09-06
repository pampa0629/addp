// 注意：预览组件（MarkdownPreview、OfficePreview 等）包含重型依赖（marked、mammoth、jszip 等），
// 已移至 ./previews 单独入口，避免不需要预览的模块引入这些依赖。
// 如需预览组件，请从 '@common-ui/previews' 导入。

// Basic UI Components (no map dependencies)
export { default as StorageEngineForm } from './components/StorageEngineForm.vue'
export { default as ResourceTree } from './components/ResourceTree.vue'
export { default as ResourceTreePicker } from './components/ResourceTreePicker.vue'
export { default as AuthLoginFlow } from './components/AuthLoginFlow.vue'
export { default as StatusAnnouncer } from './components/StatusAnnouncer.vue'
export { default as ExecutionParameterForm } from './components/ExecutionParameterForm.vue'
export { default as TabularResultRenderer } from './components/TabularResultRenderer.vue'
export { default as DataPagination } from './components/DataPagination.vue'
export { default as ExportDialog } from './components/ExportDialog.vue'
export { default as ScalarValueRenderer } from './components/ScalarValueRenderer.vue'
export {
  formatResultCell,
  isStructuredResultValue,
  lastPage,
  normalizeTabularColumns,
  paginateRows,
  safeStructuredResultJSON,
  tabularCellValue
} from './utils/tabularResult'
export { formatScalarValue, validateScalarValueResult } from './utils/scalarValueResult.mjs'
export {
  defaultFieldPresentation,
  fieldPresentationFor,
  fieldPresentationLabel,
  formatFieldPresentationValue
} from './utils/fieldPresentation.mjs'

// Schedule Components
export { default as ScheduleConfig } from './components/ScheduleConfig.vue'
export { default as ScheduleDisplay } from './components/ScheduleDisplay.vue'

// Utils
export { formatBytes, formatDate, safeStringify } from './utils/formatters'
export * from './utils/schedule'
export * from './utils/index'
export * from './utils/exportSession'
export * from './utils/fieldTypes'
export * from './utils/dynamicSchemaColumns'
export * from './utils/engineDisplay'
export * from './utils/engineAvailability'
export * from './utils/transientRequest'
export * from './utils/consoleBridge'
export * from './utils/taskOwnerUrl'
export * from './composables/useConsolePageDescriptor'
export * from './utils/moduleRouteNavigation'
export * from './utils/managerDataExplorerRoute'
export * from './utils/recoverableRouteState'
export * from './utils/resourceSelection'
export * from './utils/resourceCandidateSelection.mjs'
export * from './utils/continuousExecution'
export * from './utils/executionLineagePresentation'
export * from './utils/focus'
export * from './utils/latestRequest'
export { toAmisResponse, toAmisListResponse, createAmisInterceptor } from './utils/amis-adaptor'

// Types
export * from './types/index'
export * from './types/tree'
// resourceLocator 已经通过 types/index 导出，无需重复

// Resource capability APIs
export { detectTableMetadata, getResourceFields, getResourceItemByCatalogPath } from './api/resourceCapability'

// Resource Tree API
export {
  getResourceTree,
  getResourceTreeAncestors,
  getResourceTreeNode,
  listResourceTreeEngines,
  refreshResourceTreeNode,
  searchResourceTree
} from './api/resourceTree'

// Composables - Authentication
export {
  createAuthGuard,
  createAuthInterceptor,
  createAuthenticatedFetch,
  createRefreshInterceptor,
  buildLoginRedirectURL,
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
