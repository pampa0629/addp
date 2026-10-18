import { test } from 'node:test'
import assert from 'node:assert/strict'
import { updateDAGItemTheme } from '../../dag/src/utils/theme.js'

test('DAG theme refresh updates cached styles and restores every active state on the same item', () => {
  const selected = { x: 10, y: 20, params: { input: 'kept' } }
  const item = { model: selected, states: ['selected', 'invalid'], getStates() { return this.states } }
  const calls = []
  const appearance = { stateStyles: { selected: { stroke: 'new-selection' }, invalid: { stroke: 'new-invalid' } } }
  const graph = {
    clearItemStates(value) { assert.equal(value, item); calls.push('clear'); value.states = [] },
    updateItem(value, style) { assert.equal(value, item); assert.equal(style, appearance); calls.push('update') },
    setItemState(value, state, active) { assert.equal(value, item); assert.equal(active, true); calls.push(state); value.states.push(state) }
  }
  updateDAGItemTheme(graph, item, appearance)
  assert.deepEqual(calls, ['clear', 'update', 'selected', 'invalid'])
  assert.deepEqual(item.states, ['selected', 'invalid'])
  assert.equal(item.model, selected)
  assert.deepEqual(selected, { x: 10, y: 20, params: { input: 'kept' } })
})
