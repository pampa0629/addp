import client from './client'

const unwrap = (promise, defaultValue) =>
  promise.then(res => res.data?.data ?? defaultValue)

export const getEngines = () => unwrap(client.get('/meta/engines'), [])

export const getSchemas = engineId =>
  unwrap(client.get(`/meta/schemas/${engineId}`), [])

export const listAvailableSchemas = engineId =>
  unwrap(client.get(`/meta/schemas/${engineId}/available`), [])

export const autoScan = () => client.post('/meta/scan/auto').then(res => res.data)

export const scanEngine = (engineId, schemaNames) =>
  client
    .post('/meta/scan/engine', {
      engine_id: engineId,
      schema_names: schemaNames
    })
    .then(res => res.data)

export const getScanTasks = async engineId => {
  const tasks = await unwrap(client.get('/meta/scan/tasks'), [])
  if (!engineId) {
    return tasks
  }
  return tasks.filter(task => task.engine_id === engineId)
}

export const createScanTask = (engineId, payload) => {
  return client
    .post('/meta/scan/tasks', {
      ...payload,
      engine_id: engineId
    })
    .then(res => res.data?.data)
}

export const updateScanTask = (engineId, taskId, payload) => {
  return client
    .put(`/meta/scan/tasks/${taskId}`, {
      ...payload,
      engine_id: engineId
    })
    .then(res => res.data?.data)
}

export const deleteScanTask = taskId =>
  client.delete(`/meta/scan/tasks/${taskId}`).then(res => res.data)

export const triggerScanTask = taskId =>
  client.post(`/meta/scan/tasks/${taskId}/trigger`).then(res => res.data?.data)

export const getScanRuns = async (engineId, params = {}) => {
  const response = await client.get('/meta/scan/runs', { params })
  const runs = response.data?.data ?? []
  if (!engineId) {
    return runs
  }
  return runs.filter(run => run.engine_id === engineId)
}

export const getScanRun = runId =>
  client.get(`/meta/scan/runs/${runId}`).then(res => res.data?.data)

export const createManualScanRun = (engineId, payload = {}) =>
  client
    .post('/meta/scan/run/manual', {
      engine_id: engineId,
      ...payload
    })
    .then(res => res.data?.data)

export default {
  getEngines,
  getSchemas,
  listAvailableSchemas,
  autoScan,
  scanEngine,
  getScanTasks,
  createScanTask,
  updateScanTask,
  deleteScanTask,
  triggerScanTask,
  getScanRuns,
  getScanRun,
  createManualScanRun
}
