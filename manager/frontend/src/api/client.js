import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Manager',
  timeout: 60000
})

export default client
