import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Quality'
})

export const refreshAuthorizationOnForbidden = apiClient => {
  apiClient.interceptors.response.use(undefined, async (error) => {
    if (error?.response?.status === 403) {
      try {
        await useAuthStore().refreshAuthorization()
      } catch {
        // Preserve the owner request error; the page renders the actionable message.
      }
    }
    return Promise.reject(error)
  })
  return apiClient
}

refreshAuthorizationOnForbidden(client)

export default client
