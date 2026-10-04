import assert from 'node:assert/strict'
import { test } from 'node:test'
import { clearRuntimeAccessToken, createBrowserAuthSession, getAccessToken } from '../src/auth/authSession.js'

for (const rejected of [false, true]) {
  test(`logout submits the current token before clearing and broadcasts after ${rejected ? '401' : 'success'}`, async t => {
    const messages = []
    const browserWindow = new EventTarget()
    browserWindow.self = browserWindow
    browserWindow.top = browserWindow
    class Channel {
      addEventListener() {}
      postMessage(message) { messages.push(message) }
      close() {}
    }
    const globals = { window: browserWindow, document: {}, BroadcastChannel: Channel }
    const descriptors = new Map(Object.keys(globals).map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]))
    for (const [key, value] of Object.entries(globals)) Object.defineProperty(globalThis, key, { configurable: true, writable: true, value })
    const failure = Object.assign(new Error('authentication_required'), { status: 401 })
    const session = createBrowserAuthSession({ revoke: async token => {
      assert.equal(token, 'current-access-token')
      assert.equal(getAccessToken(), token)
      if (rejected) throw failure
    } })
    t.after(() => {
      session.dispose()
      clearRuntimeAccessToken()
      for (const [key, descriptor] of descriptors) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor)
        else delete globalThis[key]
      }
    })
    session.acceptToken('old-access-token', 900)
    session.acceptToken('current-access-token', 900)
    if (rejected) await assert.rejects(session.logout(), error => error === failure)
    else await session.logout()
    assert.equal(getAccessToken(), null)
    assert.equal(messages.at(-1).type, 'logout')
  })
}
