import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'
import { getCurrentLang } from '@common-ui/composables/useAddpI18n'
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Ontology',
  baseURL: '/api/v1/ontology',
  extractData: false
})
client.interceptors.request.use((config) => {
  config.headers['Accept-Language'] = getCurrentLang()
  return config
})
export default client
