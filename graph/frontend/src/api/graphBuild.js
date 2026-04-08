import client from './client'

export const buildAPI = {
  // ——— 构建任务 ———
  listTasks(graphId) {
    return client.get(`/graph/graphs/${graphId}/build/tasks`)
  },
  createTask(graphId, data) {
    return client.post(`/graph/graphs/${graphId}/build/tasks`, data)
  },
  getTask(graphId, taskId) {
    return client.get(`/graph/graphs/${graphId}/build/tasks/${taskId}`)
  },
  deleteTask(graphId, taskId) {
    return client.delete(`/graph/graphs/${graphId}/build/tasks/${taskId}`)
  },
  runTask(graphId, taskId) {
    return client.post(`/graph/graphs/${graphId}/build/tasks/${taskId}/run`)
  },
  cancelTask(graphId, taskId) {
    return client.post(`/graph/graphs/${graphId}/build/tasks/${taskId}/cancel`)
  },
  rerunTask(graphId, taskId) {
    return client.post(`/graph/graphs/${graphId}/build/tasks/${taskId}/rerun`)
  },

  // ——— 材料管理 ———
  listMaterials(graphId, taskId) {
    return client.get(`/graph/graphs/${graphId}/build/tasks/${taskId}/materials`)
  },
  uploadMaterials(graphId, taskId, formData, onProgress) {
    return client.post(`/graph/graphs/${graphId}/build/tasks/${taskId}/materials`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
      onUploadProgress: onProgress
    })
  },
  deleteMaterial(graphId, taskId, materialId) {
    return client.delete(`/graph/graphs/${graphId}/build/tasks/${taskId}/materials/${materialId}`)
  },

  // ——— 审核队列 ———
  listReviewItems(graphId, params = {}) {
    return client.get(`/graph/graphs/${graphId}/review`, { params })
  },
  getPendingCount(graphId) {
    return client.get(`/graph/graphs/${graphId}/review/pending-count`)
  },
  approveItem(graphId, itemId) {
    return client.post(`/graph/graphs/${graphId}/review/${itemId}/approve`)
  },
  rejectItem(graphId, itemId) {
    return client.post(`/graph/graphs/${graphId}/review/${itemId}/reject`)
  },
  modifyItem(graphId, itemId, finalContent) {
    return client.put(`/graph/graphs/${graphId}/review/${itemId}`, { final_content: finalContent })
  },
  batchReview(graphId, ids, action) {
    return client.post(`/graph/graphs/${graphId}/review/batch`, { ids, action })
  }
}
