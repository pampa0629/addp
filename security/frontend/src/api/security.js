import client from './client'

const resource = path => ({
  list: () => client.get(`/security/${path}`),
  get: id => client.get(`/security/${path}/${id}`),
  create: data => client.post(`/security/${path}`, data),
  update: (id, data) => client.put(`/security/${path}/${id}`, data),
  delete: id => client.delete(`/security/${path}/${id}`)
})

export const classificationAPI = resource('classifications')
export const gradeAPI = resource('grades')
export const sensitiveDataTypeAPI = resource('sensitive-data-types')
export const protectionBaselineAPI = {
  ...resource('protection-baselines'),
  delete: (id, data) => client.delete(`/security/protection-baselines/${id}`, { data })
}

export const protectionEnrollmentAPI = {
  list: params => client.get('/security/protection-enrollments', { params }),
  get: id => client.get(`/security/protection-enrollments/${id}`),
  create: data => client.post('/security/protection-enrollments', data),
  release: (id, data) => client.post(`/security/protection-enrollments/${id}/releases`, data),
  rediscover: (id, data) => client.post(`/security/protection-enrollments/${id}/discovery-executions`, data)
}

export const findingAPI = {
  list: params => client.get('/security/findings', { params }),
  get: id => client.get(`/security/findings/${id}`),
  review: (id, data) => client.post(`/security/findings/${id}/reviews`, data)
}

export const metaAPI = {
  getItem: itemId => client.get(`/meta/items/${itemId}`)
}
