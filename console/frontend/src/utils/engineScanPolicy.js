import { createManualScanRun, deleteEngineScanTask, getScanTasks, upsertEngineScanTask } from '../api/meta'

const normalizeScanConfig = (scanConfig) => {
  if (!scanConfig) return null
  return {
    ...scanConfig,
    enabled: Boolean(scanConfig.immediate_scan || scanConfig.scheduled_scan)
  }
}

const scanConfigFromTask = (task) => {
  if (!task) return null
  const parameters = task.parameters || {}
  return {
    enabled: true,
    immediate_scan: true,
    immediate_depth: 'basic',
    scheduled_scan: Boolean(task.enabled),
    schedule_mode: 'cron',
    cron_expression: task.schedule || '',
    schedule_time: '00:00',
    schedule_value: [],
    scan_depth: parameters.scan_depth || 'deep'
  }
}

export const handleEngineScanPolicyBridge = async (payload = {}) => {
  if (payload.action === 'load') {
    const engineId = Number(payload.engineId)
    if (!engineId) throw new Error('engine_id is required')
    const tasks = await getScanTasks(engineId)
    const task = tasks.find(item => item.owner_module === 'system' && item.owner_ref === `engine:${engineId}`)
    return { scanConfig: scanConfigFromTask(task) }
  }

  if (payload.action === 'sync') {
    const engine = payload.engine || {}
    const engineId = Number(engine.id)
    if (!engineId) throw new Error('engine_id is required')
    const scanConfig = normalizeScanConfig(payload.scanConfig)
    if (engine.engine_type !== 'postgresql' && (!scanConfig || !scanConfig.scheduled_scan)) {
      await deleteEngineScanTask(engineId)
    } else {
      await upsertEngineScanTask(engineId, {
        engine_name: engine.name,
        scan_policy: scanConfig
      })
    }

    if (payload.triggerImmediate && scanConfig?.immediate_scan) {
      await createManualScanRun(engineId, {
        scan_depth: scanConfig.immediate_depth || scanConfig.scan_depth || 'basic',
        trigger_type: 'manual',
        source: 'console',
        force: false
      })
    }

    return {}
  }

  throw new Error(`unsupported action: ${payload.action || ''}`)
}
