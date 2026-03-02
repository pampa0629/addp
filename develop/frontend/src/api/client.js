import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Develop',
  timeout: 300000  // 5分钟超时（用于长SQL查询）
})

export default client
