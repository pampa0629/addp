import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../stores/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Orchestrator'
})

export default client
