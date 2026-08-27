import client from './client'
export const listViews = (params) => client.get('/api/v1/workbench/views', { params })
export const getView = (id) => client.get(`/api/v1/workbench/views/${id}`)
export const createView = (body) => client.post('/api/v1/workbench/views', body)
export const updateView = (id, body) => client.put(`/api/v1/workbench/views/${id}`, body)
export const deleteView = (id) => client.delete(`/api/v1/workbench/views/${id}`)
