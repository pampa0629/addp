import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'
import { normalizeStandardBlobError } from '../utils/apiError'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Standard'
})

client.interceptors.response.use(undefined, async (error) => {
  await normalizeStandardBlobError(error)
  return Promise.reject(error)
})

export default client
