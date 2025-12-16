import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../stores/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Develop',
  timeout: 300000,  // 5分钟超时（用于长SQL查询）
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api'
})

export default client
