import client from './client'

export default {
  // 服务管理 CRUD

  listServices(params) {
    return client.get('/service/graph', { params })
  },

  getService(id) {
    return client.get(`/service/graph/${id}`)
  },

  createService(data) {
    return client.post('/service/graph', data)
  },

  updateService(id, data) {
    return client.put(`/service/graph/${id}`, data)
  },

  deleteService(id) {
    return client.delete(`/service/graph/${id}`)
  },

  // 图查询执行端点
  executeQuery(serviceName, data) {
    return client.post(`/gquery/${serviceName}`, data)
  },

  // 获取 Neo4j 引擎列表（通过 system 模块）
  getNeo4jEngines() {
    return client.get('/system/engines', {
      params: { engine_type: 'neo4j' }
    })
  },

  // 获取 graph item 的节点形状列表（通过 service 后端代理读取 meta type_info.graph）
  getNodeShapes(engineId, database = 'neo4j') {
    return client.get('/service/graphs/node-shapes', {
      params: { engine_id: engineId, database }
    }).then(res => {
      return Array.isArray(res) ? res : (res?.data || res?.items || [])
    })
  }
}
