import assert from 'node:assert/strict'
import test from 'node:test'
import { focusElement } from '../src/utils/focus.js'

test('late initial dialog focus preserves a user-selected control', () => {
  const input = { matches: () => true }
  const dialog = { contains: element => element === input }
  let calls = 0
  const element = { ownerDocument: { activeElement: input }, closest: () => dialog }
  const target = { $el: element, focus: () => { calls += 1 } }
  assert.equal(focusElement(target, { preserveDialogFocus: true }), false)
  assert.equal(calls, 0)
  assert.equal(focusElement(target), true)
  assert.equal(calls, 1)
})

test('initial dialog focus still runs when only the container has focus', () => {
  const dialog = { contains: () => true }
  const document = { activeElement: dialog }
  let options
  const target = { ownerDocument: document, closest: () => dialog, focus: value => { options = value } }
  assert.equal(focusElement(target, { preserveDialogFocus: true }), true)
  assert.deepEqual(options, { preventScroll: true })
})

test('a programmatically focused container inside a dialog does not count as user input', () => {
  let calls = 0
  const target = {
    ownerDocument: { activeElement: { matches: () => false } },
    closest: () => ({ contains: () => true }),
    focus: () => { calls += 1 }
  }
  assert.equal(focusElement(target, { preserveDialogFocus: true }), true)
  assert.equal(calls, 1)
})

test('focus in a different dialog does not block the newly opened dialog', () => {
  let calls = 0
  const target = { ownerDocument: { activeElement: {} }, closest: () => ({ contains: () => false }), focus: () => { calls += 1 } }
  assert.equal(focusElement(target, { preserveDialogFocus: true }), true)
  assert.equal(calls, 1)
})

test('ordinary focus and DOM-less callers retain existing behavior', () => {
  assert.equal(focusElement(null, { preserveDialogFocus: true }), false)
  let options
  assert.equal(focusElement({ focus: value => { options = value } }, { preserveDialogFocus: true, preventScroll: false }), true)
  assert.deepEqual(options, { preventScroll: false })
})
