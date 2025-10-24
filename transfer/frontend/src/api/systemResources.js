import client from './client'

export const systemResourcesAPI = {
  list: (params = {}) => client.get('/system-resources', { params })
}

