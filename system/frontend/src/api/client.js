import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'System',
  timeout: 10000
})

// These writes must never be replayed with another access token after a 401.
export const explicitWriteClient = createAPIClient(() => useAuthStore(), {
  moduleName: 'System',
  timeout: 10000,
  enableRefresh: false
})

export default client
