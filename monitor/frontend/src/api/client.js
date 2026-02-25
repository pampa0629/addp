import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Monitor'
})

export default client
