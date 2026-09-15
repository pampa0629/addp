import { afterEach, describe, expect, it, vi } from 'vitest'
import { syncConsoleRoute } from '../../../common-frontend/basic/src/utils/taskOwnerUrl'
import { requestConsoleBridge, registerConsoleBridgeHandler } from '../../../common-frontend/basic/src/utils/consoleBridge'

function runtime() {
  const listeners = new Set()
  const parent = { postMessage: vi.fn() }
  vi.stubGlobal('window', {
    parent, setTimeout, clearTimeout,
    addEventListener: (_name, fn) => listeners.add(fn),
    removeEventListener: (_name, fn) => listeners.delete(fn)
  })
  return { parent, listeners, receive: data => [...listeners].forEach(fn => fn(data)) }
}
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

describe('navigation transport versus human confirmation', () => {
  it('ignores forged responses, then waits beyond the transport timeout after a pending receipt', async () => {
    vi.useFakeTimers()
    const env = runtime()
    const result = requestConsoleBridge('navigation', {}, { timeout: 100, allowPending: true })
    const request = env.parent.postMessage.mock.calls[0][0]
    const data = { type: 'addp:console-bridge:response', channel: 'navigation', requestId: request.requestId }
    env.receive({ source: {}, data: { ...data, ok: true, data: 'forged' } })
    expect(env.listeners.size).toBe(1)
    env.receive({ source: env.parent, data: { ...data, pending: true } })
    await vi.advanceTimersByTimeAsync(10000)
    expect(env.listeners.size).toBe(1)
    env.receive({ source: env.parent, data: { ...data, ok: true, data: { cancelled: true } } })
    await expect(result).resolves.toEqual({ cancelled: true })
    expect(env.listeners.size).toBe(0)
  })

  it('propagates a rejected route synchronization instead of reporting success', async () => {
    const env = runtime()
    const result = syncConsoleRoute('/orchestrator/orchestrations')
    const request = env.parent.postMessage.mock.calls[0][0]
    env.receive({ source: env.parent, data: {
      type: 'addp:console-bridge:response', channel: request.channel,
      requestId: request.requestId, ok: true, data: { cancelled: true }
    } })
    await expect(result).resolves.toBe(false)
    expect(env.listeners.size).toBe(0)
  })

  it('still fails and removes the listener when the host never acknowledges', async () => {
    vi.useFakeTimers()
    const env = runtime()
    const failure = requestConsoleBridge('navigation', {}, { timeout: 100, allowPending: true }).catch(error => error)
    await vi.advanceTimersByTimeAsync(101)
    expect((await failure).message).toBe('Console bridge request timed out')
    expect(env.listeners.size).toBe(0)
  })

  it('acknowledges a handled request before the user decision and returns its cancellation', async () => {
    const env = runtime()
    let decide
    const handler = vi.fn(() => new Promise(resolve => { decide = resolve }))
    const stop = registerConsoleBridgeHandler('navigation', handler, { acknowledgePending: true, allowedSources: ['addp-module'] })
    const source = { postMessage: vi.fn() }
    env.receive({ source, origin: 'http://module.invalid', data: {
      type: 'addp:console-bridge:request', channel: 'navigation', source: 'addp-module', requestId: 'id', payload: {}
    } })
    expect(source.postMessage.mock.calls[0][0].pending).toBe(true)
    decide({ cancelled: true })
    await Promise.resolve()
    expect(source.postMessage.mock.calls[1][0]).toMatchObject({ ok: true, data: { cancelled: true } })
    stop()
    expect(env.listeners.size).toBe(0)
  })
})
