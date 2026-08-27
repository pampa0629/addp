import client from './client'

const base = '/api/v1/workbench/data_applications'

export const listDataApplications = (params) => client.get(base, { params })
export const getDataApplication = (id) => client.get(`${base}/${id}`)
export const createDataApplication = (body) => client.post(base, body)
export const updateDataApplication = (id, body) => client.put(`${base}/${id}`, body)
export const deleteDataApplication = (id, version) => client.delete(`${base}/${id}`, { data: { version } })
export const publishDataApplication = (id, version) => client.post(`${base}/${id}/publish`, { version })
export const offlineDataApplication = (id, version) => client.post(`${base}/${id}/offline`, { version })
export const getDataApplicationRuntime = (id) => client.get(`${base}/${id}/runtime`)
