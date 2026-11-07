// Read-side fixtures shared by workspace and interaction acceptance tests.
export function historyResponse(path, sessionId, messages, url) {
  const prefix = `/api/v1/agent/sessions/${sessionId}`
  const pending = messages.flatMap(message => (message.parts || [])
    .filter(part => part.type === 'interaction_ref' && part.status === 'pending'))
  const clarifications = pending.filter(part => part.kind === 'clarification')
  if (path === `${prefix}/workspace`) {
    const first = messages.find(message => message.role === 'user')
    return { goal: first?.content || (first?.parts || []).filter(part => part.type === 'text').map(part => part.text).join('\n\n'),
      pending_clarifications: clarifications.length, pending_approvals: pending.length - clarifications.length,
      clarification_id: clarifications.length === 1 ? clarifications[0].interaction_id : null }
  }
  if (path === `${prefix}/messages`) return { data: messages, page: { limit: 20, older_cursor: null, newer_cursor: null } }
  if (path !== `${prefix}/message-index`) return null
  const kind = url.searchParams.get('kind')
  return { data: [...messages].reverse().map(message => {
    const entries = []
    for (const part of message.parts || []) {
      if (kind === 'pending' && part.type === 'interaction_ref' && part.status === 'pending') entries.push({ key: part.interaction_id, kind: part.kind, interaction_id: part.interaction_id })
      if (kind === 'results' && part.type === 'result_ref') entries.push({ key: `${part.owner_module}:${part.kind}:${part.ref}`, kind: 'execution', label: part.ref })
      if (part.type !== 'presentation_ref' || part.protocol !== 'a2ui' || part.interaction_id) continue
      for (const operation of part.content?.operations || []) {
        for (const component of operation.updateComponents?.components || []) {
          if (kind === 'results') entries.push({ key: part.surface_id, kind: component.component, label: component.name || '' })
          if (kind === 'resources' && component.component === 'TransferDraftSnapshot') {
            const config = JSON.parse(component.parameters).config
            for (const locator of [config.source?.locator, config.target?.parent_locator]) {
              if (locator) entries.push({ key: locator, kind: 'resource', locator })
            }
          }
        }
      }
    }
    return { message_id: message.id, protocol_message_id: message.protocol_message_id || null, created_at: message.created_at, entries }
  }), page: { limit: 20, older_cursor: null, newer_cursor: null } }
}
