import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../stores/auth'

// 🔧 开发环境：在 iframe 中时使用绝对 URL，否则使用相对路径（走 Vite 代理）
// 🔧 生产环境：始终使用相对路径（走 Nginx/Gateway）
const isDev = import.meta.env.DEV
const isInIframe = window.self !== window.top

let baseURL
if (isDev && isInIframe) {
  // 开发 + iframe：直接指向 Develop 的 Vite dev server，利用其代理到 Gateway
  baseURL = 'http://localhost:5178/api'
} else {
  // 其他情况：使用相对路径或环境变量
  baseURL = import.meta.env.VITE_API_BASE_URL || '/api'
}

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Develop',
  timeout: 300000,  // 5分钟超时（用于长SQL查询）
  baseURL
})

export default client
