import client from './client'

// 门户首页
export const homeAPI = {
  getData: () => client.get('/api/portal/home')
}

// 搜索
export const searchAPI = {
  search: (params) => client.get('/api/portal/search', { params })
}

// 目录浏览
export const catalogAPI = {
  list: () => client.get('/api/portal/catalogs'),
  getAssets: (id, params) => client.get(`/api/portal/catalogs/${id}/assets`, { params })
}

// 资产详情
export const assetAPI = {
  get: (id) => client.get(`/api/portal/assets/${id}`),
  apply: (id, data) => client.post(`/api/portal/assets/${id}/apply`, data),
  getRatings: (id) => client.get(`/api/portal/assets/${id}/ratings`),
  addRating: (id, data) => client.post(`/api/portal/assets/${id}/ratings`, data)
}

// 我的申请
export const myApplicationAPI = {
  list: () => client.get('/api/portal/my/applications')
}

// 我的授权
export const myAuthorizationAPI = {
  list: () => client.get('/api/portal/my/authorizations')
}
