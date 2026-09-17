import assert from 'node:assert/strict'
import test from 'node:test'
import {
  fieldPresentationFor,
  fieldPresentationLabel,
  formatFieldPresentationValue,
  presentFieldValue,
  valueLabelsValid,
} from '../src/utils/fieldPresentation.mjs'

const presentations = [
  { field: 'amount', label: '订单金额', unit: '元', precision: 2 },
  { field: 'created_at', label: '创建时间', temporal_format: 'datetime' },
]

test('period display distinguishes a month from the whole range without changing the date', () => {
  const raw = '2024-01-01'
  const presentation = { temporal_format: 'period', period_context: { grain: 'month' } }
  assert.equal(formatFieldPresentationValue(raw, presentation, 'zh-CN'), '2024年1月')
  assert.equal(formatFieldPresentationValue(raw, presentation, 'en'), 'January 2024')
  presentation.period_context = { grain: 'total', total_label: 'Selected period total' }
  assert.equal(formatFieldPresentationValue(raw, presentation, 'en'), 'Selected period total')
  assert.equal(formatFieldPresentationValue(null, presentation), '—')
  assert.equal(formatFieldPresentationValue(raw, { temporal_format: 'period' }), '—')
  assert.equal(formatFieldPresentationValue('2024-01-01', { temporal_format: 'month' }, 'zh-CN'), '2024年1月')
  assert.equal(raw, '2024-01-01')
})

test('resolves one field presentation without changing the field identity', () => {
  assert.deepEqual(fieldPresentationFor('amount', presentations), presentations[0])
  assert.equal(fieldPresentationFor('missing', presentations), null)
  assert.equal(fieldPresentationLabel('amount', presentations, [{ name: 'amount', comment: '原始注释' }]), '订单金额')
  assert.equal(fieldPresentationLabel('status', [], [{ name: 'status', comment: '状态' }]), '状态')
})

test('value labels are exact typed display values and states still use the original value', () => {
  const presentation = {
    value_labels: [{ value: 'forward', label: 'A → B' }, { value: false, label: 'Disabled' }, { value: '', label: 'Empty' }],
    state_rules: [{ operator: 'eq', operand: 'forward', label: 'Selected', tone: 'info' }],
  }
  assert.deepEqual(presentFieldValue('forward', presentation), { text: 'A → B', state: { label: 'Selected', tone: 'info' } })
  for (const value of ['Forward', ' forward', 'missing', 'false']) assert.equal(formatFieldPresentationValue(value, presentation), value)
  assert.equal(formatFieldPresentationValue(false, presentation), 'Disabled')
  assert.equal(formatFieldPresentationValue('', presentation), 'Empty')
  assert.equal(formatFieldPresentationValue(null, presentation), '—')
})

test('value label validation rejects duplicates, unsupported types and unbounded mappings', () => {
  const label = (value, text = 'Name') => ({ value, label: text })
  assert.equal(valueLabelsValid([label(''), label('a'), label(' a'), label('A')], 'string'), true)
  assert.equal(valueLabelsValid([label(true), label(false)], 'bool'), true)
  for (const [labels, type] of [
    [[label('a'), label('a')], 'string'], [[label(false), label(false)], 'bool'],
    [[label(null)], 'string'], [[label('true')], 'bool'], [[label(1)], 'int'],
    [[label('a', ' ')], 'string'], [[label('a', 'x'.repeat(101))], 'string'],
    [[label('x'.repeat(1001))], 'string'], [Array.from({ length: 33 }, (_, i) => label(String(i))), 'string'],
  ]) assert.equal(valueLabelsValid(labels, type), false)
})

test('formats numeric and temporal values through the same controlled contract', () => {
  assert.equal(formatFieldPresentationValue(1234.5, presentations[0], 'zh-CN'), '1,234.50 元')
  assert.equal(formatFieldPresentationValue(null, presentations[0], 'zh-CN'), '—')
  assert.match(formatFieldPresentationValue('2026-09-06T12:34:56+08:00', presentations[1], 'zh-CN'), /2026/)
  assert.equal(formatFieldPresentationValue('not-a-date', presentations[1], 'zh-CN'), 'not-a-date')
})

test('resolves the first matching controlled state rule while preserving the formatted value', () => {
  const presentation = {
    field: 'score', label: '得分', unit: '分', precision: 1,
    state_rules: [
      { operator: 'lt', operand: 60, label: '不合格', tone: 'danger' },
      { operator: 'lt', operand: 80, label: '待提升', tone: 'warning' },
      { operator: 'gte', operand: 80, label: '良好', tone: 'success' },
    ],
  }

  assert.deepEqual(presentFieldValue(72.25, presentation, 'zh-CN'), {
    text: '72.3 分', state: { label: '待提升', tone: 'warning' },
  })
  assert.deepEqual(presentFieldValue(88, presentation, 'zh-CN'), {
    text: '88.0 分', state: { label: '良好', tone: 'success' },
  })
  assert.deepEqual(presentFieldValue(null, presentation, 'zh-CN'), { text: '—', state: null })
})

test('matches exact scalar states without coercing unrelated values', () => {
  const presentation = {
    field: 'status', label: '状态',
    state_rules: [{ operator: 'eq', operand: 'blocked', label: '已阻断', tone: 'danger' }],
  }
  assert.deepEqual(presentFieldValue('blocked', presentation), {
    text: 'blocked', state: { label: '已阻断', tone: 'danger' },
  })
  assert.deepEqual(presentFieldValue('ready', presentation), { text: 'ready', state: null })
})
