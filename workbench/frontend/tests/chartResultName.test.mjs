import assert from 'node:assert/strict'
import test from 'node:test'
import { chartResultName } from '../src/utils/chartResultName.mjs'
import { buildRendererConfig, draftFromComponent } from '../src/utils/componentDraft.mjs'

test('result name uses an explicit field and one complete successful result', () => {
  const rows = [{ title: 'Current person', value: 0 }, { title: 'Current person', value: 3 }]
  assert.deepEqual(chartResultName(rows, 'title', true, false), { status: 'ready', value: 'Current person' })
  assert.equal(chartResultName(rows, '', true, false), null)
  assert.equal(chartResultName(rows, 'title', false, false), null)
  assert.equal(chartResultName([], 'title', true, false), null)
  assert.deepEqual(chartResultName(rows, 'title', true, true), { status: 'ambiguous' })
  for (const names of [['A', 'B'], ['A', null], ['A', ''], ['A', 1], ['A', ' A']]) {
    assert.deepEqual(chartResultName(names.map(title => ({ title })), 'title', true, false), { status: 'ambiguous' })
  }
  assert.deepEqual(chartResultName([{ title: null }, { title: '' }], 'title', true, false), { status: 'missing' })
})

test('explicit result name and its field presentation survive edit and clone without affecting queries', () => {
  const config = { chart_type: 'bar', dimension: 'date', measures: ['value'], result_name_field: 'title', field_presentations: [{ field: 'date', label: 'Date' }, { field: 'value', label: 'Count', precision: 0 }, { field: 'title', label: 'Person' }] }
  const component = { title: 'Chart', renderer_type: 'chart', query_template: { select: ['date', 'value', 'title'] }, renderer_config: config }
  const descriptor = { input_contract: { page: { default_limit: 50 } }, output_contract: { fields: [{ name: 'date', type: 'date' }, { name: 'value', type: 'int' }, { name: 'title', type: 'string' }] } }
  const draft = draftFromComponent(component, descriptor)
  assert.deepEqual(buildRendererConfig(draft), config)
  draft.resultNameField = ''
  assert.equal(buildRendererConfig(draft).result_name_field, undefined)
  assert.deepEqual(buildRendererConfig(draft).field_presentations.map(item => item.field), ['date', 'value'])
  assert.equal(component.renderer_config.result_name_field, 'title')
})
