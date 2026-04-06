import client from './client'
import { knowledgeGraphAPI } from './ontology'

export const knowledgeServiceApi = {
  // 更新图谱（含 is_public 状态）
  updateGraph: (graphId, data) =>
    client.put(`/graph/graphs/${graphId}`, data),

  // 获取本体（接口文档展示用）
  getOntology: (graphId) =>
    client.get(`/graph/kg/${graphId}/ontology`),

  // 全文搜索（接口测试 Tab 用）
  searchEntities: (graphId, q, type = '') => {
    const params = new URLSearchParams({ q, page: 1, page_size: 10 })
    if (type) params.append('type', type)
    return client.get(`/graph/kg/${graphId}/search?${params}`)
  },

  // 图谱列表（选择图谱侧边栏用）
  listGraphs: () =>
    knowledgeGraphAPI.list(),
}
