import client from './client'

// 创建 ad-hoc execution
export const createExecution = (payload) => client.post('/develop/executions', payload)

// 执行历史列表
export const listExecutions = (params) => client.get('/develop/executions', { params })

// 获取执行详情
export const getExecution = (id) => client.get(`/develop/executions/${id}`)

// 获取执行日志
export const getExecutionLogs = (id) => client.get(`/develop/executions/${id}/logs`)

// 重试执行
export const retryExecution = (id) => client.post(`/develop/executions/${id}/retry`)

// 获取执行统计
export const getExecutionStatistics = (params) => client.get('/develop/executions/statistics', { params })
