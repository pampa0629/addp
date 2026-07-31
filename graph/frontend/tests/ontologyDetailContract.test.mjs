import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'vitest'

const ontologyDetailSource = readFileSync(
  new URL('../src/views/OntologyDetail.vue', import.meta.url),
  'utf8',
)
const zhCnMessages = JSON.parse(readFileSync(
  new URL('../src/i18n/zh-cn.json', import.meta.url),
  'utf8',
))
const sharedGraphMessages = JSON.parse(readFileSync(
  new URL('../../../common-frontend/graph/src/i18n/zh-cn.json', import.meta.url),
  'utf8',
))

function resolveMessage(path) {
  const merged = {
    ...sharedGraphMessages,
    ...zhCnMessages,
    graph: {
      ...sharedGraphMessages.graph,
      ...zhCnMessages.graph,
    },
  }
  return path.split('.').reduce((value, key) => value?.[key], merged)
}

test('本体详情页引用的翻译键均已注册', () => {
  const keys = [...ontologyDetailSource.matchAll(/(?<![\w.])t\('([^']+)'/g)].map(match => match[1])
  const missingKeys = [...new Set(keys)].filter(key => resolveMessage(key) === undefined)
  assert.deepEqual(missingKeys, [])
})

test('图形视图仅在可见时挂载，并具有稳定高度', () => {
  assert.match(ontologyDetailSource, /v-if="activeTab === 'graph'"/)
  assert.match(
    ontologyDetailSource,
    /\.graph-tab-container\s*\{[^}]*height:\s*clamp\(/s,
  )
})

test('节点展示字段显式配置并强制纳入搜索', () => {
  assert.match(ontologyDetailSource, /v-model="entityForm\.display_property"/)
  assert.match(ontologyDetailSource, /row\.data_type !== 'string' \|\| isDisplayProperty\(row\)/)
  assert.match(ontologyDetailSource, /property\.searchable = true/)
  assert.equal(resolveMessage('graph.ontology.displayProperty'), '节点展示字段')
})
