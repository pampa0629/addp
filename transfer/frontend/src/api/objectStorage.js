import client from './client'

export const objectStorageAPI = {
  browse: (payload) => client.post('/object-storage/browse', payload)
}
