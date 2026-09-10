import client from './client'

export function listDerivedTasks(params = {}) {
  return client.get('/manager/tasks', { params })
}

export function getDerivedTask(taskType, id) {
  return client.get(`/manager/tasks/${encodeURIComponent(taskType)}/${id}`)
}

export function executeDerivedTask(taskType, id, parameters = {}) {
  return client.post(`/manager/tasks/${encodeURIComponent(taskType)}/${id}/execute`, { parameters })
}

export function rebindDerivedTask(taskType, id, request) {
  return client.post(`/manager/tasks/${encodeURIComponent(taskType)}/${id}/rebind`, request)
}

export function deleteDerivedTask(taskType, id) {
  return client.delete(`/manager/tasks/${encodeURIComponent(taskType)}/${id}`)
}
