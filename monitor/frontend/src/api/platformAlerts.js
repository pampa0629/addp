import client from './client'
const base = '/monitor/platform'
export const platformAlertsAPI = {
  incidents: (params, signal) => client.get(`${base}/alerts`, { params, signal }),
  acknowledge: row => client.post(`${base}/alerts/${row.id}/acknowledge`, { version: row.version }),
  suppress: (row, until) => client.post(`${base}/alerts/${row.id}/suppress`, { version: row.version, suppressed_until: until }),
  destinations: signal => client.get(`${base}/alert-notification-destinations`, { signal }),
  createDestination: data => client.post(`${base}/alert-notification-destinations`, data),
  updateDestination: (id, data) => client.put(`${base}/alert-notification-destinations/${id}`, data),
  credential: (row, secret) => client.put(`${base}/alert-notification-destinations/${row.id}/credential`, { version: row.version, secret }),
  deleteDestination: row => client.delete(`${base}/alert-notification-destinations/${row.id}`, { data: { version: row.version } }),
  testDestination: row => client.post(`${base}/alert-notification-destinations/${row.id}/test`),
  deliveries: (page = 1, signal) => client.get(`${base}/alert-notification-deliveries`, { params: { page, page_size: 20 }, signal }),
  retryDelivery: row => client.post(`${base}/alert-notification-deliveries/${row.id}/retry`, { expected_manual_retry_count: row.manual_retry_count, destination_version: row.destination_version })
}
