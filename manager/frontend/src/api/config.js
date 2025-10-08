import client from './client'

export const configAPI = {
  getMapConfig() {
    return client.get('/config/map')
  }
}

export default configAPI
