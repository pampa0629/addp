import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createAutoFilter } from '../src/utils/autoFilter.mjs'

test('automatic filters combine text and cancel the queued query on immediate selection or navigation', t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let draft = '', pauses = 0
  const calls = [], filter = createAutoFilter({ apply: () => calls.push(draft), enabled: () => true, visible: () => true, pause: () => pauses++ })
  draft = 'N'; filter.queue(); t.mock.timers.tick(150)
  draft = 'Node'; filter.queue(); t.mock.timers.tick(299)
  assert.equal(filter.pending, true); assert.deepEqual(calls, [])
  t.mock.timers.tick(1)
  assert.deepEqual(calls, ['Node']); assert.equal(filter.pending, false)
  draft = 'immediate'; filter.queue(); filter.submit(); t.mock.timers.tick(300)
  assert.deepEqual(calls, ['Node', 'immediate'])
  draft = 'old route'; filter.queue(); filter.cancel(); t.mock.timers.tick(300)
  assert.deepEqual(calls, ['Node', 'immediate']); assert.equal(filter.pending, false)
  assert.equal(pauses, 4)
})

test('hidden filters preserve pending input and revoked or disposed owners never submit it', t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let visible = true, enabled = true, calls = 0
  const filter = createAutoFilter({ apply: () => calls++, enabled: () => enabled, visible: () => visible, pause: () => {} })
  filter.queue(); visible = false; filter.visibilityChanged(false); t.mock.timers.tick(1000)
  assert.equal(calls, 0); assert.equal(filter.pending, true)
  visible = true; filter.visibilityChanged(true); t.mock.timers.tick(300)
  assert.equal(calls, 1)
  filter.queue(); enabled = false; t.mock.timers.tick(300)
  assert.equal(calls, 1); assert.equal(filter.pending, false)
  filter.queue(); assert.equal(filter.pending, false)
  enabled = true; visible = false; filter.queue(); t.mock.timers.tick(300)
  assert.equal(calls, 1)
  filter.dispose(); visible = true; filter.visibilityChanged(true); filter.queue(); filter.submit(); t.mock.timers.tick(300)
  assert.equal(calls, 1); assert.equal(filter.pending, false)
})

test('Enter submits immediately and cancels the queued text without consuming IME confirmation', t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let calls = 0, prevented = 0
  const filter = createAutoFilter({ apply: () => calls++, enabled: () => true, visible: () => true, pause: () => {} })
  const event = { preventDefault: () => prevented++ }
  filter.queue()
  filter.enter({ ...event, isComposing: true })
  filter.enter({ ...event, keyCode: 229 })
  assert.equal(calls, 0); assert.equal(prevented, 0); assert.equal(filter.pending, true)
  filter.enter(event)
  assert.equal(calls, 1); assert.equal(prevented, 1); assert.equal(filter.pending, false)
  t.mock.timers.tick(300); assert.equal(calls, 1)
})

test('host service engine and alert pages consume one automatic-filter lifecycle', () => {
  for (const name of ['NodeResources', 'ProcessResources', 'EngineResources', 'PlatformAlerts']) {
    const source = readFileSync(new URL(`../../../monitor/frontend/src/views/${name}.vue`, import.meta.url), 'utf8')
    assert.match(source, /import \{ createAutoFilter \} from .*autoFilter\.mjs/)
    assert.match(source, /filterInput\.visibilityChanged\(/)
    assert.match(source, /filterInput\.dispose\(/)
    assert.match(source, /@keydown\.enter="filterInput.enter"/)
    assert.doesNotMatch(source, /debounce\(|pendingFilter|deferredFilter/)
    assert.doesNotMatch(source, /native-type="submit"[^>]*>\{\{ t\('common.search'\)/)
  }
})
