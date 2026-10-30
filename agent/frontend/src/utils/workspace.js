export function groupSessions(sessions, now = new Date()) {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const week = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6).getTime()
  const groups = new Map(['pinned', 'today', 'week', 'older'].map(key => [key, []]))
  for (const session of sessions) {
    const time = new Date(session.updated_at).getTime()
    groups.get(session.pinned ? 'pinned' : time >= today ? 'today' : time >= week ? 'week' : 'older').push(session)
  }
  return [...groups].filter(([, items]) => items.length).map(([key, items]) => ({ key, items }))
}

const resultComponents = new Set(['WorkflowDag', 'TransferDraftSnapshot', 'TransferConfigCheckReport', 'MapView', 'TablePreview'])
export function sessionResults(messages) {
  const results = new Map()
  for (const message of messages) {
    const source = String(message.protocol_message_id || message.id)
    for (const part of message.parts || []) {
      if (part.type === 'result_ref') {
        const key = `${part.owner_module}:${part.kind}:${part.ref}`
        results.set(key, { key, source, kind: 'execution', label: part.ref })
      } else if (part.type === 'presentation_ref' && part.protocol === 'a2ui' && !part.interaction_id) {
        for (const operation of part.content?.operations || []) {
          for (const component of operation.updateComponents?.components || []) {
            if (typeof part.surface_id === 'string' && resultComponents.has(component.component)) {
              const key = part.surface_id
              results.set(key, { key, source, kind: component.component, label: component.name || '' })
            }
          }
        }
      }
    }
  }
  return [...results.values()]
}

export function stepDuration(step, now = Date.now()) {
  const started = Date.parse(step.started_at), ended = step.completed_at ? Date.parse(step.completed_at) : now
  return Number.isFinite(started) && Number.isFinite(ended) ? Math.max(0, (ended - started) / 1000).toFixed(1) : null
}

export function validPlan(todos) {
  return Array.isArray(todos) && todos.length <= 12 && todos.every(item => item && typeof item.content === 'string' && item.content.trim().length > 0 && [...item.content].length <= 300 && ['pending', 'in_progress', 'completed'].includes(item.status))
}
