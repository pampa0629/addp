import client from './client'

// 开发任务列表
export const listDevTasks = (params) => client.get('/develop/task-definitions', { params })

// 创建开发任务
export const createDevTask = (data) => client.post('/develop/task-definitions', data)

// 获取开发任务详情
export const getDevTask = (id) => client.get(`/develop/task-definitions/${id}`)

// 更新开发任务
export const updateDevTask = (id, data) => client.put(`/develop/task-definitions/${id}`, data)

// 获取算子工作流 content 中的存储引擎绑定
export const getWorkflowStorageEngineBindings = (id) => (
  client.get(`/develop/task-definitions/${id}/storage-engine-bindings`)
)

// 原子替换算子工作流 content 中指向旧引擎的全部 ResourceLocator
export const rebindWorkflowStorageEngine = (id, sourceEngineId, targetEngineId) => (
  client.put(`/develop/task-definitions/${id}/storage-engine-bindings/${sourceEngineId}`, {
    target_engine_id: targetEngineId
  })
)

// 删除开发任务
export const deleteDevTask = (id) => client.delete(`/develop/task-definitions/${id}`)

// 执行开发任务
export const executeDevTask = (id, parameters = {}) => (
  client.post(`/develop/task-definitions/${id}/execute`, { parameters })
)

// 获取可用资源列表
export const listEngines = (params) => client.get('/develop/engines', { params })
