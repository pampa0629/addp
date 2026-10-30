import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

export { authAPI } from './auth'

const agentClient = createAPIClient(() => useAuthStore(), {
  moduleName: 'Agent',
  baseURL: '/api/v1',
})

export const sessionAPI = {
  list(params, options = {}) { return agentClient.get('/agent/sessions', { params, ...options }) },
  runs(id, params, options = {}) { return agentClient.get(`/agent/sessions/${id}/runs`, { params, ...options }) },
  skills() { return agentClient.get('/agent/skills') },
  get(id, options = {}) { return agentClient.get(`/agent/sessions/${id}`, options) },
  create(data) { return agentClient.post('/agent/sessions', data) },
  update(id, data) { return agentClient.patch(`/agent/sessions/${id}`, data) },
  delete(id, version) { return agentClient.delete(`/agent/sessions/${id}`, { data: { version } }) },
  getMessages(id, options = {}) { return agentClient.get(`/agent/sessions/${id}/messages`, options) },
}

export const runAPI = {
  get(id, options = {}) { return agentClient.get(`/agent/runs/${id}`, options) },
  cancel(id) {
    return agentClient.post(`/agent/runs/${id}/cancel`)
  },
}

export const resultRefAPI = {
  getDevelopExecution(executionId) {
    return agentClient.get(`/develop/executions/${encodeURIComponent(executionId)}`)
  }
}

export default agentClient
