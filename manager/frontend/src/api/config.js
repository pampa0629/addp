import client from './client'

export const configAPI = {
  getMapConfig() {
    return client.get('/manager/config/map')
  }
}

export default configAPI
