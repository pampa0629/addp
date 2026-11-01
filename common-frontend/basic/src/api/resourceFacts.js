import { createAPIClient, getBoundAuthStore } from '../composables/useAuth.js'

const http = createAPIClient(getBoundAuthStore, {
  moduleName: 'ResourceFacts', baseURL: '', extractData: false
})

// The existing owner endpoint returns metadata only; do not call data.preview.
export async function getResourceFacts(apiBaseUrl, locator) {
  const response = await http.get(`${apiBaseUrl}/resource-facts`, { params: { locator } })
  return response.data
}
