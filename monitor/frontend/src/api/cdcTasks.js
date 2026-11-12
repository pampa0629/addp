import client from './client'

export const cdcTasksAPI = {
  list: (page, config) => client.get('/transfer/task-definitions', { ...config, params: { change_detection: 'cdc', runtime_boundary: 'continuous', page, page_size: 20 } }),
  get: (id, config) => client.get(`/transfer/task-definitions/${encodeURIComponent(id)}`, config)
}
