import { requestConsoleBridge, toConsoleBridgeValue } from './consoleBridge.js'

export function copyNavigationDraft(value) {
  const raw = JSON.stringify(toConsoleBridgeValue(value))
  if (!raw || new TextEncoder().encode(raw).length > 4096) throw new Error('invalid_navigation_draft')
  const result = JSON.parse(raw)
  if (!result || typeof result !== 'object' || Array.isArray(result) || typeof result.kind !== 'string' || !/^[a-z][a-z0-9.-]{0,79}\/v[1-9][0-9]*$/u.test(result.kind)) throw new Error('invalid_navigation_draft')
  return result
}

export function createNavigationDraftSlot(now = Date.now) {
  let entry = null, sequence = 0, timer = null
  function clear(ticket) {
    if (ticket !== undefined && entry?.ticket !== ticket) return
    clearTimeout(timer); timer = null; entry = null
  }
  return {
    publish(route, identity, value) {
      const draft = copyNavigationDraft(value), ticket = ++sequence
      clear()
      entry = { route, identity, draft, expires: now() + 60000, ticket }
      timer = setTimeout(() => clear(ticket), 60000)
      timer.unref?.()
      return ticket
    },
    clear,
    retain(route, identity) { if (entry && (entry.route !== route || entry.identity !== identity || entry.expires <= now())) clear() },
    take(route, identity) {
      const value = entry; clear()
      return value && value.route === route && value.identity === identity && value.expires > now() ? copyNavigationDraft(value.draft) : null
    }
  }
}

export function takeConsoleNavigationDraft() {
  return requestConsoleBridge('console-navigation', { operation: 'take-draft' }, { allowPending: true })
}
