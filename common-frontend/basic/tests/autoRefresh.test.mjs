import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createAutoRefresh } from '../src/utils/autoRefresh.mjs'

function fixture() {
  const target = new EventTarget(), pending = new Map()
  target.hidden = false
  let id = 0, interval = '15', enabled = true, reads = 0, pauses = 0, visible = true
  const timers = { setTimeout(fn, delay) { pending.set(++id, { fn, delay }); return id }, clearTimeout(id) { pending.delete(id) } }
  const cycle = createAutoRefresh({ document: target, timers, interval: () => interval, enabled: () => enabled, refresh: () => { reads++ }, pause: () => { pauses++ }, visibilityChanged: value => { visible = value } })
  return { cycle, pending, setInterval: value => { interval = value }, enable: value => { enabled = value }, get reads() { return reads }, get pauses() { return pauses }, get visible() { return visible }, visibility(value) { target.hidden = value; target.dispatchEvent(new Event('visibilitychange')) }, tick() { const [id, { fn }] = pending.entries().next().value; pending.delete(id); fn() } }
}

test('one completion-owned timer rechecks permission, interval and busy state before firing', () => {
  const f = fixture()
  f.cycle.schedule(); f.cycle.schedule()
  assert.equal(f.pending.size, 1)
  assert.equal([...f.pending.values()][0].delay, 15000)
  f.tick()
  assert.equal(f.reads, 1)
  assert.equal(f.pending.size, 0) // A slow owner does not start another request.
  f.setInterval('30'); f.cycle.schedule()
  assert.equal([...f.pending.values()][0].delay, 30000)
  f.enable(false); f.tick()
  assert.equal(f.reads, 1)
  f.enable(true)
  for (const value of ['off', '1', '0', 15, undefined]) {
    f.setInterval(value); f.cycle.schedule(); assert.equal(f.pending.size, 0)
  }
  f.cycle.dispose()
})

test('hidden pages cancel pending work, resume once and off/disposed pages never resume', () => {
  const f = fixture()
  f.cycle.schedule(); f.visibility(true)
  assert.equal(f.pending.size, 0); assert.equal(f.pauses, 1); assert.equal(f.visible, false)
  f.cycle.schedule(); assert.equal(f.pending.size, 0)
  f.visibility(false); assert.equal(f.reads, 1); assert.equal(f.visible, true)
  f.setInterval('off'); f.visibility(true); f.visibility(false); assert.equal(f.reads, 1)
  f.setInterval('15'); f.cycle.schedule()
  const late = [...f.pending.values()][0].fn
  f.cycle.dispose(); late(); f.visibility(true); f.visibility(false); f.cycle.schedule()
  assert.equal(f.reads, 1); assert.equal(f.pending.size, 0); assert.equal(f.pauses, 2)
})

test('resource pages share one refresh control, timer lifecycle and translated vocabulary', () => {
  for (const page of ['NodeResources', 'ProcessResources']) {
    const source = readFileSync(new URL(`../../../monitor/frontend/src/views/${page}.vue`, import.meta.url), 'utf8')
    assert.match(source, /createAutoRefresh/); assert.match(source, /<AutoRefreshControl/)
    assert.doesNotMatch(source, /setTimeout|setInterval|addEventListener\('visibilitychange'|monitor\.resources\.refresh/)
  }
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(readFileSync(new URL(`../src/i18n/${locale}.json`, import.meta.url)))
    assert.deepEqual(Object.keys(messages.autoRefresh).sort(), ['automatic', 'interval', 'manual', 'off', 'paused', 'seconds', 'stopped'])
    const module = JSON.parse(readFileSync(new URL(`../../../monitor/frontend/src/i18n/${locale}.json`, import.meta.url)))
    assert.equal(module.monitor.resources.refreshInterval, undefined)
  }
})
