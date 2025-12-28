import client from './client'

const unwrap = (promise, defaultValue) =>
  promise.then(res => res.data?.data ?? defaultValue)

export const getEngines = () => unwrap(client.get('/meta/engines'), [])

export const getSchemas = resourceId =>
  unwrap(client.get(`/meta/schemas/${resourceId}`), [])

export const listAvailableSchemas = resourceId =>
  unwrap(client.get(`/meta/schemas/${resourceId}/available`), [])

export const autoScan = () => client.post('/meta/scan/auto').then(res => res.data)

export const scanResource = (resourceId, schemaNames) =>
  client
    .post('/meta/scan/resource', {
      resource_id: resourceId,
      schema_names: schemaNames
    })
    .then(res => res.data)

export const getScanTasks = async resourceId => {
  const tasks = await unwrap(client.get('/meta/scan/tasks'), [])
  if (!resourceId) {
    return tasks
  }
  return tasks.filter(task => task.resource_id === resourceId)
}

export const createScanTask = (resourceId, payload) => {
  return client
    .post('/meta/scan/tasks', {
      ...payload,
      resource_id: resourceId
    })
    .then(res => res.data?.data)
}

export const updateScanTask = (resourceId, taskId, payload) => {
  return client
    .put(`/meta/scan/tasks/${taskId}`, {
      ...payload,
      resource_id: resourceId
    })
    .then(res => res.data?.data)
}

export const deleteScanTask = taskId =>
  client.delete(`/meta/scan/tasks/${taskId}`).then(res => res.data)

export const triggerScanTask = taskId =>
  client.post(`/meta/scan/tasks/${taskId}/trigger`).then(res => res.data?.data)

export const getScanRuns = async (resourceId, params = {}) => {
  const response = await client.get('/meta/scan/runs', { params })
  const runs = response.data?.data ?? []
  if (!resourceId) {
    return runs
  }
  return runs.filter(run => run.resource_id === resourceId)
}

export const getScanRun = runId =>
  client.get(`/meta/scan/runs/${runId}`).then(res => res.data?.data)

export const createManualScanRun = (resourceId, payload = {}) =>
  client
    .post('/meta/scan/run/manual', {
      resource_id: resourceId,
      ...payload
    })
    .then(res => res.data?.data)

export default {
  getResources,
  getSchemas,
  listAvailableSchemas,
  autoScan,
  scanResource,
  getScanTasks,
  createScanTask,
  updateScanTask,
  deleteScanTask,
  triggerScanTask,
  getScanRuns,
  getScanRun,
  createManualScanRun
}
