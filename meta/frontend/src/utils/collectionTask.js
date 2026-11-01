export function isEngineCollectionTask(task, engineID) {
  return String(task?.engine_id) === String(engineID) && task?.scope?.type === 'engine'
}

export function collectionTaskPayload(engine, form, task = null) {
  if (engine?.resource_type !== 'postgresql' || !engine.id || !form.name?.trim()) throw new Error('invalidCollectionTask')
  if (task && (!isEngineCollectionTask(task, engine.id) || task.parameters?.scan_depth !== 'basic')) throw new Error('unsupportedCollectionTask')
  const schedule = form.scheduled ? String(form.schedule || '').trim() : ''
  if (form.scheduled && !schedule) throw new Error('missingCollectionSchedule')
  return {
    name: form.name.trim(), description: task?.description || '',
    scope: { type: 'engine', engine_id: engine.id }, scan_depth: 'basic',
    force: task?.parameters?.force === true, schedule, enabled: form.scheduled === true
  }
}

export async function saveCollectionTask(api, engine, form, task) {
  const payload = collectionTaskPayload(engine, form, task)
  return task
    ? api.updateScanTask(engine.id, task.id, payload)
    : api.createScanTask(engine.id, payload)
}
