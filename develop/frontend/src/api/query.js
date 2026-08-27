import client from './client'
import {
  createDevTask,
  updateDevTask
} from './devTask'
import { toQueryDevTaskPayload } from '@/utils/queryTaskPayload.mjs'

/**
 * 为所选数据资源获取查询模板
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

export const preflightQuery = (payload) => {
  return client.post('/develop/query-preflight', payload)
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
