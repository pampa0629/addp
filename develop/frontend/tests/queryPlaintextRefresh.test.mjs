import {describe,it} from 'node:test'
import assert from 'node:assert/strict'
import {createQueryPlaintextRefresh} from '../src/utils/queryPlaintextRefresh.mjs'

describe('current plaintext query result authorization', () => {
  it('discards an in-flight recheck after the user clears or replaces the result', async () => {
    let resolve
    const h = deadlineHarness(() => new Promise(done => { resolve = done }))
    h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(20000).toISOString() })
    const pending = h.fire(6000)
    h.refresh.stop()
    resolve({ plaintext: true })
    await Promise.all(pending)
    assert.deepEqual(h.events, [])
    assert.equal(h.timers.size, 0)
  })
  it('clears plaintext at its deadline before reading the current protected result', async () => {
    const h = deadlineHarness(async () => { h.events.push('read'); return { masked: true } })
    h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(2000).toISOString() })
    await Promise.all(h.fire(2000))
    assert.deepEqual(h.events, ['clear', 'read', { masked: true }])
    assert.equal(h.timers.size, 0)
  })
  it('clears stale values on a failed authorization recheck and stops after unmount', async () => {
    const h = deadlineHarness(async () => { throw Error('denied') })
    h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(20000).toISOString() })
    await Promise.all(h.fire(6000))
    assert.deepEqual(h.events, ['clear'])
    h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(20000).toISOString() })
    h.refresh.dispose()
    await Promise.all(h.fire(20000))
    assert.deepEqual(h.events, ['clear'])
    assert.equal(h.timers.size, 0)
  })
})


function deadlineHarness(fetchExecution) {
  let clock = 1000
  let id = 0
  const timers = new Map()
  const events = []
  const refresh = createQueryPlaintextRefresh({
    now: () => clock,
    setTimer: (fn, ms) => { const key = ++id; timers.set(key, { fn, at: clock + ms }); return key },
    clearTimer: key => timers.delete(key),
    fetchExecution, normalize: value => value,
    onClear: () => events.push('clear'), onResult: value => events.push(value)
  })
  return { refresh, events, timers, fire(time) {
    clock = time
    const due = [...timers].filter(([, timer]) => timer.at <= time)
    return due.map(([key, timer]) => { timers.delete(key); return timer.fn() })
  } }
}

it('clears at the plaintext deadline while a recheck is still pending and discards its expired raw response', async () => {
  let resolve
  const h = deadlineHarness(() => new Promise(done => { resolve = done }))
  h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(10000).toISOString() })
  const pending = h.fire(6000)
  assert.equal(pending.length, 1)
  await Promise.all(h.fire(10000))
  assert.deepEqual(h.events, ['clear'], 'a hanging recheck must not extend visible plaintext lifetime')
  resolve({ execution_id: 'one', plaintext_expires_at: new Date(10000).toISOString(), raw: true })
  await Promise.all(pending)
  assert.deepEqual(h.events, ['clear'], 'late expired plaintext must never reappear')
  assert.equal(h.timers.size, 0)
})

it('does not display a raw recheck response whose own deadline has already passed', async () => {
  let resolve
  const h = deadlineHarness(() => new Promise(done => { resolve = done }))
  h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(20000).toISOString() })
  const pending = h.fire(6000)
  h.fire(9000)
  resolve({ execution_id: 'one', plaintext_expires_at: new Date(8000).toISOString(), raw: true })
  await Promise.all(pending)
  assert.deepEqual(h.events, ['clear'])
  assert.equal(h.timers.size, 0)
})


it('clears an already expired initial result synchronously and only accepts the protected refresh', async () => {
  const h = deadlineHarness(async () => ({ execution_id: 'one', masked: true }))
  h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(500).toISOString() })
  assert.deepEqual(h.events, ['clear'])
  await Promise.all(h.fire(1000))
  assert.deepEqual(h.events, ['clear', { execution_id: 'one', masked: true }])
  assert.equal(h.timers.size, 0)
})

it('can display the protected fallback after clearing the deadline of an in-flight raw recheck', async () => {
  let resolve
  const h = deadlineHarness(() => new Promise(done => { resolve = done }))
  h.refresh.schedule({ execution_id: 'one', plaintext_expires_at: new Date(10000).toISOString() })
  const pending = h.fire(6000)
  h.fire(10000)
  resolve({ execution_id: 'one', masked: true })
  await Promise.all(pending)
  assert.deepEqual(h.events, ['clear', { execution_id: 'one', masked: true }])
  assert.equal(h.timers.size, 0)
})
