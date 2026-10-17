import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { readGraphTheme, getContrastingTextColor, graphLabelColor } from '../src/graphTheme.js'

test('图主题读取调用元素继承的 CSS 变量，不缓存旧主题', t => {
  const root = {}
  const variables = new Map([
    ['--addp-graph-category-1', ' #2563EB '],
    ['--addp-graph-category-12', '#475569'],
    ['--addp-graph-node-stroke', '#FFFFFF'],
    ['--addp-graph-edge-label-stroke', '#FFFFFF']
  ])
  const original = globalThis.getComputedStyle
  globalThis.getComputedStyle = element => {
    assert.equal(element, root)
    return { getPropertyValue: name => variables.get(name) || '' }
  }
  t.after(() => {
    if (original) globalThis.getComputedStyle = original
    else delete globalThis.getComputedStyle
  })
  assert.deepEqual(readGraphTheme(root).categoryColors, ['#2563EB', '#475569'])
  assert.equal(readGraphTheme(root).nodeStroke, '#FFFFFF')
  variables.set('--addp-graph-node-stroke', '#374151')
  variables.set('--addp-graph-edge-label-stroke', '#1D1E1F')
  assert.equal(readGraphTheme(root).nodeStroke, '#374151')
  assert.equal(readGraphTheme(root).edgeLabelStroke, '#1D1E1F')
})

test('节点标签按填充亮度选择主题文字色', () => {
  const light = '#FFFFFF'
  const dark = '#111827'
  assert.equal(getContrastingTextColor('#FFFFFF', light, dark), dark)
  assert.equal(getContrastingTextColor('#111827', light, dark), light)
  assert.equal(getContrastingTextColor('#fff', light, dark), dark)
  assert.equal(getContrastingTextColor('#FF9DA7', light, dark), dark)
  assert.equal(getContrastingTextColor('#D97706', light, dark), dark)
})

test('主题读取和标签对比色由共享 graph 包唯一提供', () => {
  const encoding = readFileSync(new URL('../../../graph/frontend/src/utils/graphVisualEncoding.js', import.meta.url), 'utf8')
  assert.doesNotMatch(encoding, /function (readGraphTheme|getContrastingTextColor)\b/)
  for (const path of ['components/GraphCanvas.vue', 'views/GraphBrowser.vue']) {
    const source = readFileSync(new URL(`../../../graph/frontend/src/${path}`, import.meta.url), 'utf8')
    assert.match(source, /import \{ getContrastingTextColor, readGraphTheme \} from '@addp\/common-frontend\/graph'/)
  }
})

test('结构与查询结果共享稳定的标签配色，主题换色不改变标签分组', () => {
  const firstPalette = ['#111111', '#222222', '#333333']
  const nextPalette = ['#444444', '#555555', '#666666']
  const color = graphLabelColor('Person', firstPalette)
  assert.ok(firstPalette.includes(color))
  assert.equal(graphLabelColor('Person', firstPalette), color)
  assert.equal(graphLabelColor('Person', nextPalette), nextPalette[firstPalette.indexOf(color)])
  for (const file of ['GraphResultView.vue', 'GraphSchemaView.vue']) {
    const source = readFileSync(new URL(`../src/${file}`, import.meta.url), 'utf8')
    assert.match(source, /import \{[^}]*graphLabelColor[^}]*\} from '\.\/graphTheme\.js'/)
    assert.doesNotMatch(source, /function labelColor\b|const palette\s*=/)
  }
})
