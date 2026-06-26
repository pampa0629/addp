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
 * @param {number|null} engineId - 数据源ID；DuckDB 联邦查询传 null
 * @param {string} sql - SQL语句
 * @param {number} timeout - 超时时间（秒）
 * @param {string} queryMode - 查询模式；DuckDB 联邦查询使用 duckdb
 */
export const executeQuery = (engineId, sql, timeout = 30, queryMode = '') => {
  const mode = (queryMode || '').trim().toLowerCase()
  const executionConfig = mode ? { query_mode: mode } : { engine_id: engineId }
  return client.post('/develop/execute', {
    content: {
      query: sql,
      query_type: 'sql'
    },
    execution_config: executionConfig,
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
  const queryMode = (taskData.query_mode || '').trim().toLowerCase()
  const executionConfig = {}
  if (queryMode) {
    executionConfig.query_mode = queryMode
  } else {
    executionConfig.engine_id = taskData.engine_id
  }

  const payload = {
    name: taskData.name,
    display_name: taskData.display_name,
    content: {
      query: taskData.query,
      query_type: queryType
    },
    execution_config: executionConfig,
    timeout: taskData.timeout,
    description: taskData.description,
    tags: taskData.tags
  }

  if (includeDevType) {
    payload.dev_type = 'query'
  }

  return payload
}
