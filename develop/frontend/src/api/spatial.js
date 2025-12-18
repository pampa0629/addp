/**
 * 空间工作流 API
 */
import client from './client'

/**
 * 获取支持空间工作流的引擎列表
 * @returns {Promise} 返回支持workflow开发模式的引擎列表
 */
export function listSpatialEngines() {
  return client.get('/develop/spatial/engines')
}

/**
 * 获取所有空间算子列表
 */
export function listOperators() {
  return client.get('/develop/spatial/operators')
}

/**
 * 执行单个算子
 * @param {string} operatorName - 算子名称
 * @param {object} params - 参数
 */
export function executeOperator(operatorName, params) {
  return client.post(`/develop/spatial/operators/${operatorName}/execute`, { params })
}

/**
 * 即时执行工作流
 * @param {object} workflowDef - 工作流定义
 * @param {object} inputData - 输入数据
 */
export function executeWorkflow(workflowDef, inputData = {}) {
  return client.post('/develop/spatial/workflow/execute', {
    workflow_def: workflowDef,
    input_data: inputData
  })
}

/**
 * 获取任务列表
 * @param {number} page - 页码
 * @param {number} pageSize - 每页数量
 * @param {Object} filters - 过滤条件 { name: string, status: string }
 */
export function listTasks(page = 1, pageSize = 20, filters = {}) {
  return client.get('/develop/spatial/tasks', {
    params: {
      page,
      page_size: pageSize,
      name: filters.name || undefined,
      status: filters.status || undefined
    }
  })
}

/**
 * 获取任务详情
 * @param {number} id - 任务ID
 */
export function getTask(id) {
  return client.get(`/develop/spatial/tasks/${id}`)
}

/**
 * 创建任务（保存工作流为任务）
 * @param {object} taskData - 任务数据
 */
export function createTask(taskData) {
  return client.post('/develop/spatial/tasks', taskData)
}

/**
 * 更新任务
 * @param {number} id - 任务ID
 * @param {object} taskData - 任务数据
 */
export function updateTask(id, taskData) {
  return client.put(`/develop/spatial/tasks/${id}`, taskData)
}

/**
 * 删除任务
 * @param {number} id - 任务ID
 */
export function deleteTask(id) {
  return client.delete(`/develop/spatial/tasks/${id}`)
}

/**
 * 执行已保存的任务
 * @param {number} id - 任务ID
 * @param {object} inputs - 输入参数
 */
export function executeTask(id, inputs = {}) {
  return client.post(`/develop/spatial/tasks/${id}/execute`, { inputs })
}

/**
 * 查询执行状态
 * @param {string} executionId - 执行ID
 */
export function getExecutionStatus(executionId) {
  return client.get(`/develop/spatial/executions/${executionId}`)
}

/**
 * 获取 GIS 执行列表
 * @param {number} page - 页码
 * @param {number} pageSize - 每页数量
 * @param {object} filters - 筛选条件
 */
export function listExecutions(page = 1, pageSize = 20, filters = {}) {
  return client.get('/develop/gis-executions', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    }
  })
}

/**
 * 获取 GIS 执行详情
 * @param {number} id - 执行ID
 */
export function getExecution(id) {
  return client.get(`/develop/gis-executions/${id}`)
}

/**
 * 获取 GIS 执行日志
 * @param {number} id - 执行ID
 * @param {boolean} download - 是否下载
 */
export function getExecutionLogs(id, download = false) {
  return client.get(`/develop/gis-executions/${id}/logs`, {
    params: { download: download ? 'true' : 'false' }
  })
}

/**
 * 重试 GIS 执行
 * @param {number} id - 执行ID
 */
export function retryExecution(id) {
  return client.post(`/develop/gis-executions/${id}/retry`)
}

/**
 * 取消 GIS 执行
 * @param {number} id - 执行ID
 */
export function cancelExecution(id) {
  return client.post(`/develop/gis-executions/${id}/cancel`)
}

/**
 * 删除 GIS 执行记录
 * @param {number} id - 执行ID
 */
export function deleteExecution(id) {
  return client.delete(`/develop/gis-executions/${id}`)
}

/**
 * 获取 GIS 执行统计信息
 */
export function getExecutionStatistics() {
  return client.get('/develop/gis-executions/statistics')
}
