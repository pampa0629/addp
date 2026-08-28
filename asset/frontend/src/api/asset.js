import client from './client'

// 资产类型管理
export const typeDefinitionAPI = {
  list: () => client.get('/asset/type-definitions'),
  get: (id) => client.get(`/asset/type-definitions/${id}`)
}

// 资产分类管理
export const categoryAPI = {
  tree: () => client.get('/asset/categories/tree'),
  list: () => client.get('/asset/categories'),
  get: (id) => client.get(`/asset/categories/${id}`),
  create: (data) => client.post('/asset/categories', data),
  update: (id, data) => client.put(`/asset/categories/${id}`, data),
  delete: (id, version) => client.delete(`/asset/categories/${id}`, { data: { version } })
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
  batchPublish: (ids) => client.post('/asset/assets/batch-publish', { ids }),
  batchOffline: (ids) => client.post('/asset/assets/batch-offline', { ids }),
  batchCategory: (ids, categoryId) => client.post('/asset/assets/batch-category', { ids, category_id: categoryId }),
  // 类型扩展字段定义
  typeFields: (typeId) => client.get(`/asset/assets/type-fields/${typeId}`)
}

// 企业资源目录只用于管理员显式选择资产组件。
export const enterpriseCatalogAPI = {
	list: (params) => client.get('/catalog/entries', { params }),
	get: (id) => client.get(`/catalog/entries/${id}`)
}

// 申请管理（Phase 4）
export const applicationAPI = {
  list: (params) => client.get('/asset/applications', { params }),
  get: (id) => client.get(`/asset/applications/${id}`),
  approve: (id, data) => client.post(`/asset/applications/${id}/approve`, data),
  reject: (id, data) => client.post(`/asset/applications/${id}/reject`, data),
  revokeAuth: (id) => client.post(`/asset/applications/${id}/revoke`)
}

// 评价管理（Phase 6）
export const ratingAPI = {
  list: (params) => client.get('/asset/ratings', { params }),
  markHandled: (id, isHandled) => client.post(`/asset/ratings/${id}/mark-handled`, { is_handled: isHandled })
}

// 运营看板统计（Phase 6.3）
export const statsAPI = {
  dashboard: () => client.get('/asset/assets/stats/dashboard')
}
