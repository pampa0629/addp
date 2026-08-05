import client from './client'
import {
  createDevTask,
  updateDevTask
} from './devTask'

/**
 * 获取引擎的查询模板（切换引擎或选择数据资源时填充编辑器）
 * @param {number} engineId - 引擎ID
 * @param {string} locator - 可选标准资源定位符
 * @returns {{ query: string, language: string }}
 */
export const getSampleQuery = (engineId, locator = '') => {
  return client.get(`/develop/engines/${engineId}/sample-query`, {
    params: locator ? { locator } : undefined
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
