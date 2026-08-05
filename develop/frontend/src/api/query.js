import client from './client'
import {
  createDevTask,
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
