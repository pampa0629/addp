import client from './client'

// 创建 ad-hoc execution
export const createExecution = (payload) => client.post('/develop/executions', payload)

// 从成功查询快照或完整当次工作流表格创建正式导出会话
export const createQueryExport = (id, payload, config) => client.post(`/develop/executions/${id}/exports`, payload, config)

// 获取本人正式导出会话状态
export const getQueryExport = (id, config) => client.get(`/develop/exports/${id}`, config)

// 获取执行详情
export const getExecution = (id) => client.get(`/develop/executions/${id}`)

// 获取执行日志
export const getExecutionLogs = (id) => client.get(`/develop/executions/${id}/logs`)

// 重试执行
export const retryExecution = (id) => client.post(`/develop/executions/${id}/retry`)

// 当次本人查询或私有工作流结果；历史摘要继续使用 getExecution。
export const getQueryResult = (id, config) => client.get(`/develop/executions/${id}/result`, config)
