import axios from 'axios'
import { createAuthAPI } from '@common-ui'
import { getCurrentLang } from '@common-ui/composables/useAddpI18n'
const client = axios.create({ baseURL: '/api/v1/system', timeout: 10000 })
client.interceptors.request.use((config) => {
  config.headers['Accept-Language'] = getCurrentLang()
  return config
})
export const authAPI = createAuthAPI(client)
