import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../stores/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Service'
})

export default client
