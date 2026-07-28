/**
 * Composables 索引文件
 * 导出所有 composable 函数
 */

// Tree Management Composables
export { useTreeCache } from './useTreeCache'
export { useTreeLoader } from './useTreeLoader'

// Authentication Composables
export {
  createAuthGuard,
  createAuthInterceptor,
  createAuthenticatedFetch,
  createRefreshInterceptor,
  buildLoginRedirectURL,
  createAuthAPI,
  createAPIClient,
  createAuthStore
} from './useAuth'
export {
  createIframeAuthCoordinator,
  getAccessToken,
  getAccessTokenExpiresAt,
  setRuntimeAccessToken,
  clearRuntimeAccessToken,
  subscribeAccessToken
} from '../auth/authSession'

// I18n Composables
export {
  createAddpI18n,
  useAddpI18n,
  getCurrentLang,
  SUPPORTED_LANGS,
  SUPPORTED_LANGUAGES,
  DEFAULT_LANG
} from './useAddpI18n'
