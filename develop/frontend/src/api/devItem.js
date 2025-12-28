import client from './client'

// 开发项列表
export const listDevItems = (params) => client.get('/develop/items', { params })

// 创建开发项
export const createDevItem = (data) => client.post('/develop/items', data)

// 获取开发项详情
export const getDevItem = (id) => client.get(`/develop/items/${id}`)

// 更新开发项
export const updateDevItem = (id, data) => client.put(`/develop/items/${id}`, data)

// 删除开发项
export const deleteDevItem = (id) => client.delete(`/develop/items/${id}`)

// 执行开发项
export const executeDevItem = (id, inputs) => client.post(`/develop/items/${id}/execute`, inputs)

// 获取可用资源列表
export const listEngines = (params) => client.get('/develop/engines', { params })
