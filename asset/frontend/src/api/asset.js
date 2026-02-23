import client from './client'

// 资产类型管理
export const typeDefinitionAPI = {
  list: () => client.get('/asset/type-definitions'),
  get: (id) => client.get(`/asset/type-definitions/${id}`)
}

// 目录管理
export const catalogAPI = {
  tree: () => client.get('/asset/catalogs/tree'),
  list: () => client.get('/asset/catalogs'),
  get: (id) => client.get(`/asset/catalogs/${id}`),
  create: (data) => client.post('/asset/catalogs', data),
  update: (id, data) => client.put(`/asset/catalogs/${id}`, data),
  delete: (id) => client.delete(`/asset/catalogs/${id}`)
}

// 资产管理
export const assetAPI = {
  list: (params) => client.get('/asset/assets', { params }),
  get: (id) => client.get(`/asset/assets/${id}`),
  create: (data) => client.post('/asset/assets', data),
  update: (id, data) => client.put(`/asset/assets/${id}`, data),
  delete: (id) => client.delete(`/asset/assets/${id}`),
  // 状态流转（简化版：draft ↔ published ↔ offline）
  publish: (id) => client.post(`/asset/assets/${id}/publish`),
  offline: (id) => client.post(`/asset/assets/${id}/offline`),
  // 批量操作
  sync: () => client.post('/asset/assets/sync'),
  batchPublish: (ids) => client.post('/asset/assets/batch-publish', { ids }),
  batchOffline: (ids) => client.post('/asset/assets/batch-offline', { ids }),
  batchCatalog: (ids, catalogId) => client.post('/asset/assets/batch-catalog', { ids, catalog_id: catalogId }),
  // 类型扩展字段定义
  typeFields: (typeId) => client.get(`/asset/assets/type-fields/${typeId}`)
}

// 申请管理（Phase 4）
export const applicationAPI = {
  list: (params) => client.get('/asset/applications', { params }),
  get: (id) => client.get(`/asset/applications/${id}`),
  create: (data) => client.post('/asset/applications', data),
  approve: (id, data) => client.post(`/asset/applications/${id}/approve`, data),
  reject: (id, data) => client.post(`/asset/applications/${id}/reject`, data)
}

// 授权管理（Phase 5）
export const authorizationAPI = {
  list: (params) => client.get('/asset/authorizations', { params }),
  get: (id) => client.get(`/asset/authorizations/${id}`),
  revoke: (id) => client.post(`/asset/authorizations/${id}/revoke`)
}

// 评价管理（Phase 6）
export const ratingAPI = {
  list: (params) => client.get('/asset/ratings', { params }),
  create: (data) => client.post('/asset/ratings', data),
  update: (id, data) => client.put(`/asset/ratings/${id}`, data)
}
