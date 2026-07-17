import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const systemClient = createAPIClient(() => useAuthStore(), {
  moduleName: 'Meta',
  baseURL: '/api/v1/system',
  timeout: 30000,
  extractData: true
})

export default {
  // 获取引擎列表
  getEngines(params) {
    return systemClient.get('/engines', { params })
  },

  // 获取单个引擎
  getEngine(id) {
    return systemClient.get(`/engines/${id}`)
  }
}
