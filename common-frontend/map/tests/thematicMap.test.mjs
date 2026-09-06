import assert from 'node:assert/strict'
import test from 'node:test'
import { buildThematicContext, thematicColorVariable, thematicIndexForValue } from '../src/utils/thematicMap.mjs'

const features = [1, 3, 5, 7, 9].map((value) => ({ properties: { metric: value, category: `C${value}` } }))

test('builds a controlled continuous scale from an explicitly configured field', () => {
  const context = buildThematicContext(features, { mode: 'continuous', field: 'metric', palette: 'primary' })
  assert.equal(context.valid, true)
  assert.equal(context.entries.length, 5)
  assert.equal(thematicIndexForValue(9, context), 4)
  assert.equal(thematicColorVariable(4, 5, 'success'), '--el-color-success-light-2')
})

test('builds bounded categories and rejects invalid or excessive values', () => {
  const context = buildThematicContext(features, { mode: 'categorical', field: 'category', palette: 'warning' })
  assert.deepEqual(context.categories, ['C1', 'C3', 'C5', 'C7', 'C9'])
  assert.equal(thematicIndexForValue('C5', context), 2)

  const excessive = Array.from({ length: 9 }, (_, index) => ({ properties: { category: `C${index}` } }))
  assert.equal(buildThematicContext(excessive, { mode: 'categorical', field: 'category', palette: 'primary' }).reason, 'category_limit')
  assert.equal(buildThematicContext([{ properties: { metric: null } }], { mode: 'continuous', field: 'metric', palette: 'primary' }).reason, 'invalid_measure')
})

test('formats thematic legend values with the shared field presentation', () => {
  const context = buildThematicContext(features, {
    mode: 'continuous', field: 'metric', palette: 'primary',
  }, { field: 'metric', label: '指标', unit: '分', precision: 1 }, 'zh-CN')

  assert.match(context.entries[0].label, /分/)
  assert.match(context.entries[0].label, /1\.0/)
})
