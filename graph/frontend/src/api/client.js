import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Graph'
})

export default client
