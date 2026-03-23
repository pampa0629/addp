import client from './client'

export function importData(formData) {
  return client.post('/manager/import', formData, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
}

export function getTransferExecutionStatus(executionId) {
  return client.get(`/transfer/executions/${executionId}`)
}
