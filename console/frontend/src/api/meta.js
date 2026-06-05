import client from './client'

const unwrap = (promise, defaultValue) =>
  promise.then(res => res?.data?.data ?? res?.data ?? res ?? defaultValue)

const unwrapData = res => res?.data?.data ?? res?.data ?? res

export const getEngines = () => unwrap(client.get('/meta/engines'), [])

export const getSchemas = engineId =>
  client.get(`/meta/engines/${engineId}/tree`).then(res => {
    const nodes = res.data?.top_nodes ?? res.data?.data?.top_nodes ?? []
    return nodes.map(node => ({
      id: node.id,
      name: node.name,
      schema_name: node.name,
      node_type: node.node_type,
      path: node.path || node.full_name || node.name,
      scan_status: node.scan_status,
      scanned_at: node.scanned_at,
      item_count: node.item_count || 0,
      total_size_bytes: node.total_size_bytes || 0
    }))
  })

export const listAvailableSchemas = engineId =>
  client.get(`/system/engines/${engineId}/namespaces`).then(res => {
    const namespaces = res.data?.namespaces ?? res.data?.data?.namespaces ?? []
    return namespaces.map(item => ({
      ...item,
      schema_name: item.name || item.schema_name,
      name: item.name || item.schema_name
    }))
  })

export const createUnscannedScanRuns = () => client.post('/meta/scan/run/unscanned').then(res => res.data)

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
    .then(unwrapData)
}

export const updateScanTask = (engineId, taskId, payload) => {
  return client
    .put(`/meta/scan/tasks/${taskId}`, {
      ...payload,
      engine_id: engineId
    })
    .then(unwrapData)
}

export const deleteScanTask = taskId =>
  client.delete(`/meta/scan/tasks/${taskId}`).then(unwrapData)

export const triggerScanTask = taskId =>
  client.post(`/meta/scan/tasks/${taskId}/trigger`).then(unwrapData)

export const getScanRuns = async (engineId, params = {}) => {
  const response = await client.get('/meta/scan/runs', { params })
  const runs = response?.data?.data ?? response?.data ?? response ?? []
  if (!engineId) {
    return runs
  }
  return runs.filter(run => run.engine_id === engineId)
}

export const getScanRun = runId =>
  client.get(`/meta/scan/runs/${runId}`).then(unwrapData)

export const createManualScanRun = (engineId, payload = {}) =>
  client
    .post('/meta/scan/run/manual', {
      engine_id: engineId,
      ...payload
    })
    .then(unwrapData)

export const upsertEngineScanTask = (engineId, payload) =>
  client.put(`/meta/scan/tasks/engines/${engineId}`, payload)

export const deleteEngineScanTask = engineId =>
  client.delete(`/meta/scan/tasks/engines/${engineId}`)

export default {
  getEngines,
  getSchemas,
  listAvailableSchemas,
  createUnscannedScanRuns,
  getScanTasks,
  createScanTask,
  updateScanTask,
  deleteScanTask,
  triggerScanTask,
  getScanRuns,
  getScanRun,
  createManualScanRun,
  upsertEngineScanTask,
  deleteEngineScanTask
}
