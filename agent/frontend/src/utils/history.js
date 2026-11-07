import { conversationResources, messageIdentity, pendingSessionInteractions } from './conversation'
import { sessionResults } from './workspace'

export function mergeMessageWindow(existing, incoming, prepend = false) {
  const latest = new Map([...existing, ...incoming].map(message => [messageIdentity(message), message]))
  const ordered = prepend ? [...incoming, ...existing] : [...existing, ...incoming]
  const seen = new Set()
  return ordered.filter(message => {
    const key = messageIdentity(message)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  }).map(message => latest.get(messageIdentity(message)))
}

export function referenceEntries(rows) {
  const entries = new Map()
  const fraction = row => (String(row.created_at).match(/\.(\d+)/)?.[1] || '').padEnd(6, '0')
  const ordered = [...rows].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) ||
    fraction(b).localeCompare(fraction(a)) || Number(b.message_id) - Number(a.message_id))
  for (const row of ordered) {
    for (const entry of row.entries) {
      if (!entries.has(entry.key)) entries.set(entry.key, { ...entry, source: String(row.message_id) })
    }
  }
  return [...entries.values()]
}

export function conversationReferences(indexRows, messages, kind) {
  const resolved = new Set(kind === 'pending' ? messages.flatMap(message => (message.parts || [])
    .filter(part => part.type === 'interaction_ref' && part.status !== 'pending').map(part => part.interaction_id)) : [])
  const rows = messages.map(message => {
    const projected = kind === 'results' ? sessionResults([message]) : kind === 'resources'
      ? conversationResources([message]).map(item => ({ ...item, key: item.locator, kind: 'resource' }))
      : pendingSessionInteractions([message]).map(item => ({ ...item, key: item.interaction_id }))
    return { message_id: message.id, created_at: message.created_at, entries: projected }
  })
  return referenceEntries([...rows, ...indexRows]).filter(entry => !resolved.has(entry.key))
}
