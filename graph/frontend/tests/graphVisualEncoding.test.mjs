import { test } from 'vitest'
import assert from 'node:assert/strict'
import {
  createGraphVisualEncoding,
  getContrastingTextColor,
  graphNodeTypeKey
} from '../src/utils/graphVisualEncoding.js'

const palette = ['#111111', '#222222', '#333333', '#444444']

test('重复的本体颜色会按完整节点形状稳定拆分', () => {
  const input = {
    nodeShapes: [
      { name: 'Company', color: '#5B8FF9' },
      { name: 'Person+Researcher', color: '#5B8FF9' }
    ],
    palette
  }
  const first = createGraphVisualEncoding(input)
  const second = createGraphVisualEncoding(input)

  assert.notEqual(first.nodeTypes.get('Company').color, first.nodeTypes.get('Person+Researcher').color)
  assert.deepEqual([...first.nodeTypes], [...second.nodeTypes])
  assert.equal(graphNodeTypeKey({ labels: ['Researcher', 'Person'] }), 'Person+Researcher')
})

test('唯一的本体颜色被保留，关系方向和线型进入同一编码', () => {
  const encoding = createGraphVisualEncoding({
    relationshipShapes: [
      { type: 'KNOWS', color: '#ABCDEF', directed: false },
      { type: 'WORKS_AT', color: '#FEDCBA', directed: true }
    ],
    palette
  })

  assert.equal(encoding.relationshipTypes.get('KNOWS').color, '#ABCDEF')
  assert.equal(encoding.relationshipTypes.get('KNOWS').directed, false)
  assert.equal(encoding.relationshipTypes.get('WORKS_AT').directed, true)
  assert.ok(Array.isArray(encoding.relationshipTypes.get('KNOWS').lineDash))
})

test('同一 Schema 的前四种关系覆盖四种不同线型', () => {
  const encoding = createGraphVisualEncoding({
    relationshipShapes: ['A', 'B', 'C', 'D'].map(type => ({ type })),
    palette
  })
  assert.equal(new Set([...encoding.relationshipTypes.values()].map(item => item.dashIndex)).size, 4)
})

test('标签文字按填充亮度选择主题文字色', () => {
  assert.equal(getContrastingTextColor('#FFFFFF', 'light', 'dark'), 'dark')
  assert.equal(getContrastingTextColor('#111827', 'light', 'dark'), 'light')
})
