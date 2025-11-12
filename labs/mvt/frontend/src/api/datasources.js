import client from './client'

// 获取所有数据源
export const getDataSources = () => {
  return client.get('/datasources')
}

// 获取数据源详情
export const getDataSource = (id) => {
  return client.get(`/datasources/${id}`)
}

// 清空缓存
export const clearCache = (datasourceId = null) => {
  const url = datasourceId ? `/cache/clear/${datasourceId}` : '/cache/clear'
  return client.post(url)
}

// 获取缓存统计
export const getCacheStats = () => {
  return client.get('/cache/stats')
}
