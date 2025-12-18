import client from './client'

// 获取所有算子
export const listAllOperators = () => client.get('/develop/operators')

// 按模块获取算子
export const listOperatorsByModule = (module) => client.get(`/develop/operators/modules/${module}`)

// 获取算子详情
export const getOperatorDetail = (module, name) => client.get(`/develop/operators/${module}/${name}`)
