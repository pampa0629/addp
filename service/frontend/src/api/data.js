import client from './client'

export default {
  // 数据查询服务
  // 表查询
  query(data) {
    return client.post('/service/data/query', data)
  },

  // 聚合查询
  aggregate(data) {
    return client.post('/service/data/aggregate', data)
  }
}
