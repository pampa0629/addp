import client from './client'

export const systemEnginesAPI = {
  list: (params = {}) => client.get('/transfer/system-engines', { params })
}

