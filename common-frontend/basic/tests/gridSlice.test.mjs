import test from 'node:test'
import assert from 'node:assert/strict'
import { gridWindowLimit, validateGridSelection, gridColorValues } from '../src/utils/gridSlice.js'

test('grid dimensions stay exact while bounded indices are safe UI numbers', () => {
  assert.equal(gridWindowLimit('18446744073709551615', 3), 128)
  assert.equal(gridWindowLimit('7', 5), 2)
  assert.equal(gridWindowLimit('7', -1), 0)
  assert.equal(gridWindowLimit('not a size'), 0)
  const variable = { name: '/temperature', slice_supported: true, dimensions: [{ size: '2' }, { size: '100' }, { size: '100' }] }
  const selection = { variable: '/temperature', indices: [1], x: 0, y: 0, width: 64, height: 64 }
  assert.equal(validateGridSelection(variable, selection), true)
  for (const change of [{ indices: [] }, { indices: [2] }, { indices: [9007199254740992] }, { x: 100 }, { width: 100, height: 100 }, { variable: '/other' }]) {
    assert.equal(validateGridSelection(variable, { ...selection, ...change }), false)
  }
})

test('grid color approximation never consumes missing, nonfinite or unsafe integer text', () => {
  const values = [['9007199254740993', '18446744073709551615', '1.25', '2', 'Infinity', 'NaN', '-9']]
  assert.deepEqual(gridColorValues(values, [[true, true, true, true, true, true, false]]), [1.25, 2])
  assert.deepEqual(values[0].slice(0, 2), ['9007199254740993', '18446744073709551615'])
})

test('shared grid rendering extends the single container selector instead of copying it', async () => {
  const { readFile } = await import('node:fs/promises')
  const source = await readFile(new URL('../src/components/previews/GridContainerPreview.vue', import.meta.url), 'utf8')
  const container = await readFile(new URL('../src/components/previews/ContainerPreview.vue', import.meta.url), 'utf8')
  assert.match(source, /<ContainerPreview/)
  assert.doesNotMatch(source, /<el-select/)
  assert.match(container, /<slot name="child-details"/)
  assert.match(container, /<slot name="child-preview"/)
})
