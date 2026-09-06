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
export const definitionProfileAPI = {
  list: () => client.get('/security/definition-profiles'),
  apply: profileKey => client.post('/security/definition-profile-applications', { profile_key: profileKey })
}
export const detectorCapabilityAPI = { list: () => client.get('/security/detector-capabilities') }
export const detectorAPI = {
  ...resource('detectors'),
  delete: (id, data) => client.delete(`/security/detectors/${id}`, { data })
}
export const protectionBaselineAPI = {
  ...resource('protection-baselines'),
  delete: (id, data) => client.delete(`/security/protection-baselines/${id}`, { data })
}

export const protectionEnrollmentAPI = {
  list: params => client.get('/security/protection-enrollments', { params }),
  get: id => client.get(`/security/protection-enrollments/${id}`),
  components: id => client.get(`/security/protection-enrollments/${id}/components`),
  create: data => client.post('/security/protection-enrollments', data),
  reEnroll: (id, data) => client.post(`/security/protection-enrollments/${id}/re-enrollments`, data),
  release: (id, data) => client.post(`/security/protection-enrollments/${id}/releases`, data),
  rediscover: (id, data) => client.post(`/security/protection-enrollments/${id}/discovery-executions`, data)
}

export const findingAPI = {
  list: params => client.get('/security/findings', { params }),
  get: id => client.get(`/security/findings/${id}`),
  review: (id, data) => client.post(`/security/findings/${id}/reviews`, data)
}

export const discoveryQualityAPI = {
  get: params => client.get('/security/discovery-quality', { params })
}

export const assessmentAPI = {
  list: params => client.get('/security/assessments', { params }),
  create: data => client.post('/security/assessments', data),
  update: (id, data) => client.put(`/security/assessments/${id}`, data),
  revoke: (id, data) => client.delete(`/security/assessments/${id}`, { data })
}

export const protectionExemptionAPI = {
  list: params => client.get('/security/protection-exemptions', { params }),
  get: id => client.get(`/security/protection-exemptions/${id}`),
  revoke: (id, data) => client.delete(`/security/protection-exemptions/${id}`, { data })
}

export const protectionAccessRequestAPI = {
  targets: params => client.get('/security/protection-access-request-targets', { params }),
  mine: params => client.get('/security/protection-access-requests', { params }),
  reviewQueue: params => client.get('/security/protection-access-requests/review-queue', { params }),
  create: data => client.post('/security/protection-access-requests', data),
  decide: (id, data) => client.post(`/security/protection-access-requests/${id}/decisions`, data)
}

export const metaAPI = {
  getItem: itemId => client.get(`/meta/items/${itemId}`)
}
