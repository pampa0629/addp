import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Model'
})

// Permission assignments can change while the Model iframe remains open.
// Refresh the local authorization snapshot after a denied request so action
// buttons converge without requiring a full page reload, while preserving the
// original 403 for the caller to render the permission-specific message.
export const refreshAuthorizationOnForbidden = apiClient => {
  apiClient.interceptors.response.use(undefined, async (error) => {
    if (error?.response?.status === 403) {
      try {
        await useAuthStore().refreshAuthorization()
      } catch {
        // Keep the original operation error; the caller owns user feedback.
      }
    }
    return Promise.reject(error)
  })
  return apiClient
}

refreshAuthorizationOnForbidden(client)

export default client
