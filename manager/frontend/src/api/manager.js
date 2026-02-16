import client from './client'

export const managerAPI = {
  // 获取目录列表
  getDirectories(params) {
    return client.get('/manager/directories', { params })
  },

  // 创建目录
  createDirectory(data) {
    return client.post('/manager/directories', data)
  },

  // 更新目录
  updateDirectory(id, data) {
    return client.put(`/manager/directories/${id}`, data)
  },

  // 删除目录
  deleteDirectory(id) {
    return client.delete(`/manager/directories/${id}`)
  },

  // 预览数据
  previewData(params) {
    return client.get('/manager/preview', { params })
  },

  // 上传文件
  uploadFile(formData, config) {
    return client.post('/manager/upload', formData, config)
  }
}
