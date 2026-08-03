import client from './client'
import {
  createDevTask,
  deleteDevTask,
  listDevTasks,
  updateDevTask
} from './devTask'

/**
 * 获取引擎的样例查询（切换引擎时自动填充编辑器）
 * @param {number} engineId - 引擎ID
 * @returns {{ query: string, language: string }}
 */
export const getSampleQuery = (engineId) => {
  return client.get(`/develop/engines/${engineId}/sample-query`)
}

/**
 * 执行 SQL
 * @param {number} engineId - 查询引擎ID；DuckDB 联邦查询传真实 Runtime Engine ID
 * @param {string} query - 查询语句
 * @param {string} queryType - 查询语言类型
 * @param {number} timeout - 超时时间（秒）
 */
export const executeQuery = (engineId, query, queryType = 'sql', timeout = 30) => {
  return client.post('/develop/execute', {
    content: {
      query,
      query_type: queryType
    },
    execution_config: { engine_id: engineId },
    timeout: timeout
  })
}

/**
 * 测试数据库连接
 * @param {number} engineId - 数据源ID
 */
export const testConnection = (engineId) => {
  return client.get(`/develop/test/${engineId}`)
}

/**
 * 获取健康状态
 */
export const getHealth = () => {
  return client.get('/develop/health')
}

/**
 * 保存 SQL 为任务
 * @param {object} taskData - 任务数据
 */
export const saveQueryTask = (taskData) => {
  return createDevTask(toQueryDevTaskPayload(taskData))
}

/**
 * 更新 SQL 任务
 * @param {number} id - 任务ID
 * @param {object} taskData - 任务数据
 */
export const updateQueryTask = (id, taskData) => {
  return updateDevTask(id, toQueryDevTaskPayload(taskData, false))
}

/**
 * 获取 SQL 任务列表
 * @param {object} params - 查询参数
 */
export const listQueryTasks = (params = {}) => {
  return listDevTasks({
    ...params,
    dev_type: 'query'
  })
}

/**
 * 获取 SQL 任务详情
 * @param {number} id - 任务ID
 */
export const getQueryTask = (id) => {
  return client.get(`/develop/task-definitions/${id}`)
}

/**
 * 删除 SQL 任务
 * @param {number} id - 任务ID
 */
export const deleteQueryTask = (id) => {
  return deleteDevTask(id)
}

const toQueryDevTaskPayload = (taskData, includeDevType = true) => {
  const queryType = taskData.query_type || 'sql'
  const payload = {
    name: taskData.name,
    display_name: taskData.display_name,
    content: {
      query: taskData.query,
      query_type: queryType
    },
    execution_config: { engine_id: taskData.engine_id },
    timeout: taskData.timeout,
    description: taskData.description,
    tags: taskData.tags
  }

  if (includeDevType) {
    payload.dev_type = 'query'
  }

  return payload
}
