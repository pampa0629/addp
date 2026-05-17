import client from './client'

export const capabilitiesAPI = {
  get: () => client.get('/transfer/capabilities')
}
