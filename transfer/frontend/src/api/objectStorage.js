import client from './client'

export const objectStorageAPI = {
  browse: (payload) => client.post('/transfer/object-storage/browse', payload)
}
