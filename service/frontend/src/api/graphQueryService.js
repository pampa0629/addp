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
    return client.get('/system/engines').then(res => {
      // extractData=true 时 res 已是 payload
      const engines = res?.engines || (Array.isArray(res) ? res : (res?.data || []))
      return engines.filter(e =>
        e.engine_type?.toLowerCase() === 'neo4j' ||
        e.type?.toLowerCase() === 'neo4j'
      )
    })
  },

  // 获取 Neo4j 引擎的节点标签列表（通过 meta 模块）
  getNodeLabels(engineId, database = 'neo4j') {
    return client.get(`/meta/engines/${engineId}/items`, {
      params: { namespace: database }
    }).then(res => {
      const items = Array.isArray(res) ? res : (res?.items || res?.data || [])
      return items.map(t => (typeof t === 'string' ? t : t.name || t))
    })
  }
}
