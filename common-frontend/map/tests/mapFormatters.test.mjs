import assert from 'node:assert/strict'
import test from 'node:test'
import { formatFeatureProperties } from '../src/utils/mapFormatters.js'

test('uses configured field presentations for map popup labels and values', () => {
  const html = formatFeatureProperties(
    { district: '雨花区', area: 12.5, geometry: { type: 'Polygon' } },
    {
      primaryField: 'district',
      fields: ['area'],
      fieldPresentations: [
        { field: 'district', label: '行政区' },
        { field: 'area', label: '面积', unit: '亩', precision: 1 },
      ],
      locale: 'zh-CN',
    },
  )

  assert.match(html, />行政区</)
  assert.match(html, />面积:</)
  assert.match(html, />12\.5 亩</)
  assert.doesNotMatch(html, />area:</)
})

test('adds a controlled semantic state to map popup values', () => {
  const html = formatFeatureProperties(
    { district: '雨花区', area: 125, geometry: { type: 'Polygon' } },
    {
      primaryField: 'district', fields: ['area'],
      fieldPresentations: [{
        field: 'area', label: '面积', precision: 0,
        state_rules: [{ operator: 'gt', operand: 100, label: '重点关注', tone: 'danger' }],
      }],
      locale: 'zh-CN',
    },
  )

  assert.match(html, /state-indicator state--danger/)
  assert.match(html, />重点关注</)
  assert.match(html, />125 /)
})
