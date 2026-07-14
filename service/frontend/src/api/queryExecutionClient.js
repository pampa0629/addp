import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

export default createAPIClient(() => useAuthStore(), {
  moduleName: 'Service',
  baseURL: ''
})
