import assert from 'node:assert/strict'
import test from 'node:test'
import {
  fieldPresentationFor,
  fieldPresentationLabel,
  formatFieldPresentationValue,
} from '../src/utils/fieldPresentation.mjs'

const presentations = [
  { field: 'amount', label: '订单金额', unit: '元', precision: 2 },
  { field: 'created_at', label: '创建时间', temporal_format: 'datetime' },
]

test('resolves one field presentation without changing the field identity', () => {
  assert.deepEqual(fieldPresentationFor('amount', presentations), presentations[0])
  assert.equal(fieldPresentationFor('missing', presentations), null)
  assert.equal(fieldPresentationLabel('amount', presentations, [{ name: 'amount', comment: '原始注释' }]), '订单金额')
  assert.equal(fieldPresentationLabel('status', [], [{ name: 'status', comment: '状态' }]), '状态')
})

test('formats numeric and temporal values through the same controlled contract', () => {
  assert.equal(formatFieldPresentationValue(1234.5, presentations[0], 'zh-CN'), '1,234.50 元')
  assert.equal(formatFieldPresentationValue(null, presentations[0], 'zh-CN'), '—')
  assert.match(formatFieldPresentationValue('2026-09-06T12:34:56+08:00', presentations[1], 'zh-CN'), /2026/)
  assert.equal(formatFieldPresentationValue('not-a-date', presentations[1], 'zh-CN'), 'not-a-date')
})
