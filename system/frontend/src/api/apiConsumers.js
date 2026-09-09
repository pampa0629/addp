import client from './client'

export const apiConsumersAPI = {
  create(data) {
    return client.post('/system/tenant/api-consumers', data)
  },
  list() {
    return client.get('/system/tenant/api-consumers')
  },
  get(id) {
    return client.get(`/system/tenant/api-consumers/${id}`)
  },
  update(id, data) {
    return client.put(`/system/tenant/api-consumers/${id}`, data)
  },
  delete(id) {
    return client.delete(`/system/tenant/api-consumers/${id}`)
  },
  createCredential(consumerId, data) {
    return client.post(`/system/tenant/api-consumers/${consumerId}/credentials`, data)
  },
  listCredentials(consumerId) {
    return client.get(`/system/tenant/api-consumers/${consumerId}/credentials`)
  },
  revokeCredential(consumerId, credentialId) {
    return client.delete(`/system/tenant/api-consumers/${consumerId}/credentials/${credentialId}`)
  },
  listConsumableServices() {
    return client.get('/service/consumer/api-consumer-services', {
      params: { page: 1, page_size: 100 }
    })
  }
}
