import assert from 'node:assert/strict'
import { test } from 'node:test'

async function browserTabs(t) {
  const channels = new Set()
  let lock = Promise.resolve()
  const browserWindow = new EventTarget()
  browserWindow.self = browserWindow
  browserWindow.top = browserWindow
  class Channel {
    constructor() { channels.add(this) }
    addEventListener(_name, listener) { this.listener = listener }
    postMessage(message) {
      for (const other of channels) {
        if (other !== this) queueMicrotask(() => other.listener?.({ data: message }))
      }
    }
    close() { channels.delete(this) }
  }
  const globals = {
    window: browserWindow, document: {}, BroadcastChannel: Channel,
    navigator: { locks: { request(_name, _options, callback) {
      const pending = lock.then(callback)
      lock = pending.catch(() => {})
      return pending
    } } }
  }
  const descriptors = new Map(Object.keys(globals).map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]))
  for (const [key, value] of Object.entries(globals)) Object.defineProperty(globalThis, key, { configurable: true, writable: true, value })
  const sessions = []
  t.after(() => {
    for (const session of sessions) session.dispose()
    for (const [key, descriptor] of descriptors) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor)
      else delete globalThis[key]
    }
  })
  let index = 0
  return {
    async tab(token, expiresAt, refresh) {
      // Each browser tab owns a separate module runtime, but shares the channel and lock.
      const module = await import(`../src/auth/authSession.js?coordination=${encodeURIComponent(t.name)}-${index++}`)
      const session = module.createBrowserAuthSession({ refresh })
      module.setRuntimeAccessToken(token, expiresAt)
      sessions.push(session)
      return { ...module, session }
    }
  }
}

test('forced refresh does not mistake a different revoked peer reply for a server rotation', async t => {
  const tabs = await browserTabs(t)
  const expiry = Date.now() + 1_200_000
  await tabs.tab('revoked-first', expiry)
  await tabs.tab('revoked-second', expiry + 300_000)
  let refreshes = 0
  const active = await tabs.tab('revoked-first', expiry, async () => {
    refreshes++
    return { access_token: 'server-replacement', expires_in: 900 }
  })

  assert.equal(await active.session.refreshAccessToken({ force: true }), 'server-replacement')
  assert.equal(refreshes, 1)
  assert.equal(active.getAccessToken(), 'server-replacement')
})

test('concurrent forced refreshes share one rotation and all tabs receive its replacement', async t => {
  const tabs = await browserTabs(t)
  // Server policy may shorten the replacement lifetime; expiry is not a rotation sequence.
  const expiry = Date.now() + 1_200_000
  let refreshes = 0
  let release
  const pending = new Promise(resolve => { release = resolve })
  const refresh = async () => {
    refreshes++
    await pending
    return { access_token: 'server-replacement', expires_in: 900 }
  }
  const first = await tabs.tab('revoked-first', expiry, refresh)
  const second = await tabs.tab('revoked-first', expiry, refresh)
  const stale = await tabs.tab('revoked-second', expiry + 300_000)
  const requests = Promise.all([
    first.session.refreshAccessToken({ force: true }),
    second.session.refreshAccessToken({ force: true })
  ])
  release()
  assert.deepEqual(await requests, ['server-replacement', 'server-replacement'])
  assert.equal(refreshes, 1)
  assert.equal(stale.getAccessToken(), 'server-replacement')
})

test('a bootstrap peer reply cannot replace an unrelated active tab token', async t => {
  const tabs = await browserTabs(t)
  const expiry = Date.now() + 300_000
  await tabs.tab('older-peer', expiry)
  const active = await tabs.tab('server-replacement', expiry + 600_000)
  const newcomer = await tabs.tab(null, 0, async () => {
    throw new Error('bootstrap should validate the available peer token first')
  })

  assert.equal(await newcomer.session.initialize(), 'older-peer')
  assert.equal(active.getAccessToken(), 'server-replacement')
})

for (const status of [401, 503]) {
  test(`forced refresh ${status} cannot be hidden by a stale peer reply`, async t => {
    const tabs = await browserTabs(t)
    const expiry = Date.now() + 300_000
    const peer = await tabs.tab('different-revoked-peer', expiry + 300_000)
    let refreshes = 0
    const failure = Object.assign(new Error('refresh rejected'), { status })
    const active = await tabs.tab('original-token', expiry, async () => {
      refreshes++
      throw failure
    })

    await assert.rejects(active.session.refreshAccessToken({ force: true }), error => error === failure)
    assert.equal(refreshes, 1)
    assert.equal(active.getAccessToken(), status === 401 ? null : 'original-token')
    assert.equal(peer.getAccessToken(), status === 401 ? null : 'different-revoked-peer')
  })
}
