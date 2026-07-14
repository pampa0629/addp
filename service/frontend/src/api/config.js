import client from './client'

export default {
  async getMapConfig() {
    const data = await client.get('/manager/config/map')
    return { data }
  }
}
