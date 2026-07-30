import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  createBrowserAuthSession,
  getAccessToken
} from '../../../common-frontend/basic/src/auth/authSession'

describe('Browser AuthSession context switch', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('uses the shared lock order and broadcasts the replacement context token', async () => {
    const locks = []
    const messages = []
    const channelListeners = []
    const reload = vi.fn()
    const browserWindow = {
      location: { reload },
      addEventListener: vi.fn(),
      removeEventListener: vi.fn()
    }
    browserWindow.self = browserWindow
    browserWindow.top = browserWindow

    class FakeBroadcastChannel {
      addEventListener(_type, listener) {
        channelListeners.push(listener)
      }

      postMessage(message) {
        messages.push(message)
      }

      close() {}
    }

    vi.stubGlobal('window', browserWindow)
    vi.stubGlobal('document', {})
    vi.stubGlobal('BroadcastChannel', FakeBroadcastChannel)
    vi.stubGlobal('navigator', {
      locks: {
        request: async (name, _options, callback) => {
          locks.push(name)
          return callback()
        }
      }
    })

    const switchContext = vi.fn(async (token, choice) => {
      expect(token).toBe('addp_at_old')
      expect(choice).toEqual({ type: 'tenant', tenant_membership_id: '19' })
      return { data: { access_token: 'addp_at_new', expires_in: 900 } }
    })
    const session = createBrowserAuthSession({ switchContext })
    session.acceptToken('addp_at_old', 900)
    messages.length = 0

    await session.switchContext({ type: 'tenant', tenant_membership_id: '19' })

    expect(locks).toEqual(['addp-auth-refresh', 'addp-auth-context-switch'])
    expect(switchContext).toHaveBeenCalledOnce()
    expect(getAccessToken()).toBe('addp_at_new')
    expect(messages).toHaveLength(1)
    expect(messages[0]).toMatchObject({ type: 'context_changed', token: 'addp_at_new' })

    channelListeners[0]({
      data: {
        type: 'context_changed',
        token: 'addp_at_other_context',
        expiresAt: Date.now() + 900_000,
        sender: 'another-tab'
      }
    })
    expect(getAccessToken()).toBe('addp_at_other_context')
    expect(reload).toHaveBeenCalledOnce()

    session.clearToken({ broadcastEvent: false })
    session.dispose()
  })
})
