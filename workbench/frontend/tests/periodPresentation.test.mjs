import assert from 'node:assert/strict'
import test from 'node:test'
import { resolvePeriodPresentation, periodValueConfig } from '../src/utils/periodPresentation.mjs'
import { buildRendererConfig, draftFromComponent } from '../src/utils/componentDraft.mjs'

const presentation = { field: 'arbitrary_date', label: '期间', temporal_format: 'period', period: { grain_parameter: 'g', start_parameter: 's', end_parameter: 'e' } }
const config = { columns: ['arbitrary_date'], field_presentations: [presentation] }
const translate = (key, values) => values ? `${values.start} inclusive / ${values.end} exclusive` : 'Period total'

test('explicit parameter bindings drive period presentation and preserve saved config', () => {
  const query = { g: 'total', s: '2024-01-01', e: '2025-01-01' }
  const original = structuredClone(config)
  const display = resolvePeriodPresentation(config, query, 'zh-CN', translate)
  query.s = '2020-01-01'
  assert.equal(display.summaries[0], '2024/01/01 inclusive / 2025/01/01 exclusive')
  assert.equal(display.config.field_presentations[0].period_context.total_label, 'Period total')
  assert.deepEqual(config, original)
  assert.equal(resolvePeriodPresentation(config, null, 'en', translate).summaries.length, 0)
  assert.equal(resolvePeriodPresentation(config, { g: 'day', s: '2024-01-01', e: '2025-01-01' }, 'en', translate).config.field_presentations[0].period_context, null)
  assert.equal(resolvePeriodPresentation(config, { g: 'month', s: '2025-01-01', e: '2024-01-01' }, 'en', translate).summaries.length, 0)
})

test('period bindings round trip through the canonical component editor compiler', () => {
  const descriptor = { input_contract: {}, output_contract: { fields: [{ name: 'arbitrary_date', type: 'date' }] } }
  const component = { title: 'Period', renderer_type: 'table', query_template: { select: ['arbitrary_date'], page_limit: 20 }, renderer_config: config }
  const draft = draftFromComponent(component, descriptor)
  assert.deepEqual(buildRendererConfig(draft), config)
  draft.fieldPresentations[0].period.start_parameter = 'changed'
  assert.equal(config.field_presentations[0].period.start_parameter, 's')
  draft.fieldPresentations[0].temporalFormat = 'date'
  assert.equal(buildRendererConfig(draft).field_presentations[0].period, undefined)
})

test('total cards require explicit intent and completed period context, retaining measure presentation', () => {
  const chart = { chart_type: 'bar', dimension: 'arbitrary_date', measures: ['amount'], total_as_value: true,
    field_presentations: [presentation, { field: 'amount', label: 'Amount', unit: 'kg', precision: 2, state_rules: [{ operator: 'gt', operand: 10, label: 'High', tone: 'warning' }] }] }
  const original = structuredClone(chart)
  const total = resolvePeriodPresentation(chart, {g:'total',s:'2024-01-01',e:'2025-01-01'}, 'en', translate)
  assert.deepEqual(periodValueConfig(total.config).items, [chart.field_presentations[1]])
  assert.equal(periodValueConfig(chart), null)
  assert.equal(periodValueConfig({...total.config,total_as_value:false}), null)
  const monthly = resolvePeriodPresentation(chart, {g:'month',s:'2024-01-01',e:'2025-01-01'}, 'en', translate)
  assert.equal(periodValueConfig(monthly.config), null)
  assert.deepEqual(chart,original)
  const descriptor={input_contract:{},output_contract:{fields:[{name:'arbitrary_date',type:'date'},{name:'amount',type:'decimal'}]}}
  const component={title:'Totals',renderer_type:'chart',query_template:{select:['arbitrary_date','amount'],page_limit:50},renderer_config:chart}
  const draft=draftFromComponent(component,descriptor)
  assert.equal(draft.totalAsValue,true)
  assert.deepEqual(buildRendererConfig(draft),chart)
  draft.totalAsValue=false
  assert.equal(buildRendererConfig(draft).total_as_value,undefined)
})
