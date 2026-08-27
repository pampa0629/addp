import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

export default createAPIClient(() => useAuthStore(), {
  moduleName: 'Workbench',
  baseURL: '',
  extractData: false
})
