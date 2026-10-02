import client from './client'
const base = '/monitor/platform'
export const logPipelineAPI = {
  summary: signal => client.get(`${base}/log-pipeline`, { signal }),
  policy: () => client.get(`${base}/log-pipeline/policy`),
  updatePolicy: data => client.put(`${base}/log-pipeline/policy`, data),
  acknowledge: row => client.post(`${base}/log-pipeline/incidents/${row.id}/acknowledge`, { version: row.version }),
  suppress: (row, until) => client.post(`${base}/log-pipeline/incidents/${row.id}/suppress`, { version: row.version, suppressed_until: until }),
  destinations: () => client.get(`${base}/log-notification-destinations`),
  createDestination: data => client.post(`${base}/log-notification-destinations`, data),
  updateDestination: (id, data) => client.put(`${base}/log-notification-destinations/${id}`, data),
  credential: (row, secret) => client.put(`${base}/log-notification-destinations/${row.id}/credential`, { version: row.version, secret }),
  deleteDestination: row => client.delete(`${base}/log-notification-destinations/${row.id}`, { data: { version: row.version } }),
  testDestination: row => client.post(`${base}/log-notification-destinations/${row.id}/test`),
  deliveries: (page = 1) => client.get(`${base}/log-notification-deliveries`, { params: { page, page_size: 20 } })
}
