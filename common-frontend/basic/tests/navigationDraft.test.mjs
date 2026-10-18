import assert from 'node:assert/strict'
import test from 'node:test'
import { copyNavigationDraft, createNavigationDraftSlot } from '../src/utils/navigationDraft.mjs'

const draft = () => ({ kind: 'addp.host-collection/v1', endpoint: 'https://host:9100/metrics' })
test('navigation drafts are bounded, copied, expire and can only be consumed once by their route and identity', () => {
  let time = 0
  const slot = createNavigationDraftSlot(() => time), input = draft()
  slot.publish('/monitor/new', 'actor:1', input); input.endpoint = 'changed'
  assert.deepEqual(slot.take('/monitor/new', 'actor:1'), draft())
  assert.equal(slot.take('/monitor/new', 'actor:1'), null)
  for (const [route, identity] of [['/other', 'actor:1'], ['/monitor/new', 'actor:2']]) {
    slot.publish('/monitor/new', 'actor:1', draft()); assert.equal(slot.take(route, identity), null)
  }
  slot.publish('/monitor/new', 'actor:1', draft()); time = 60000
  assert.equal(slot.take('/monitor/new', 'actor:1'), null)
  assert.throws(() => copyNavigationDraft({ kind: 'x/v1', value: 'x'.repeat(4096) }))
  for (const value of [null, [], {}, { kind: 'invalid' }]) assert.throws(() => copyNavigationDraft(value))
})
test('a canceled older navigation cannot clear a newer draft; unrelated navigation and authorization changes discard it', () => {
  const slot = createNavigationDraftSlot(), old = slot.publish('/a', 'identity', draft())
  slot.publish('/b', 'identity', draft()); slot.clear(old)
  assert.deepEqual(slot.take('/b', 'identity'), draft())
  slot.publish('/a', 'identity', draft()); slot.retain('/b', 'identity')
  assert.equal(slot.take('/a', 'identity'), null)
  slot.publish('/a', 'identity', draft()); slot.retain('/a', 'revoked')
  assert.equal(slot.take('/a', 'identity'), null)
})
