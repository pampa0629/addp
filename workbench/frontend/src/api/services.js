import client from './client'
import { assertQueryOperation } from '../utils/serviceOperation.mjs'
export const listConsumerServices = (params) => client.get('/api/v1/service/consumer/services', { params })
export const getConsumerDescriptor = (ref) => client.get(`/api/v1/service/consumer/services/${ref.service_type}/${ref.service_id}`)
export const executeDescriptorOperation = (operation, body, options = {}) => {
  const queryOperation = assertQueryOperation(operation)
  return client.post(queryOperation.path, body, {
    headers: { 'X-ADDP-Query-Intent': options.intent || 'query' },
    responseType: options.responseType
  })
}
