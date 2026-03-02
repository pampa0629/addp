import client from './client'

// 门户首页
export const homeAPI = {
  getData: () => client.get('/portal/home')
}

// 搜索
export const searchAPI = {
  search: (params) => client.get('/portal/search', { params })
}

// 目录浏览
export const catalogAPI = {
  list: () => client.get('/portal/catalogs'),
  getAssets: (id, params) => client.get(`/portal/catalogs/${id}/assets`, { params })
}

// 资产详情
export const assetAPI = {
  get: (id) => client.get(`/portal/assets/${id}`),
  apply: (id, data) => client.post(`/portal/assets/${id}/apply`, data),
  getApplyStatus: (id) => client.get(`/portal/assets/${id}/apply-status`),
  getEndpoints: (id) => client.get(`/portal/assets/${id}/endpoints`),
  getRatings: (id) => client.get(`/portal/assets/${id}/ratings`),
  addRating: (id, data) => client.post(`/portal/assets/${id}/ratings`, data)
}

// 我的申请与授权
export const myApplicationAPI = {
  list: () => client.get('/portal/my/applications')
}
