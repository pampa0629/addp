import client from './client'

export const configAPI = {
  async getMapConfig() {
    const data = await client.get('/manager/config/map')
    return { data }
  }
}

export default configAPI
