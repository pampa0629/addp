import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const repositoryRoot = resolve(import.meta.dirname, '../../..')
const source = relativePath => readFileSync(resolve(repositoryRoot, relativePath), 'utf8')

test('tabular preview consumers compose the single basic table implementation', () => {
  const consumers = [
    'common-frontend/map/src/components/TablePreview.vue',
    'common-frontend/basic/src/components/previews/ContainerPreview.vue',
    'common-frontend/agent-ui/src/a2ui/components/TablePreview.vue',
    'develop/frontend/src/components/QueryResult.vue'
  ]

  for (const relativePath of consumers) {
    const content = source(relativePath)
    assert.match(content, /TabularResultRenderer/)
    assert.doesNotMatch(content, /<el-table(?:\s|>)/)
    assert.doesNotMatch(content, /<el-table-v2(?:\s|>)/)
  }
})

test('preview pagination consumers compose the single controlled pagination implementation', () => {
  const consumers = [
    'common-frontend/map/src/components/TablePreview.vue',
    'common-frontend/basic/src/components/previews/ContainerPreview.vue',
    'develop/frontend/src/components/QueryResult.vue'
  ]

  for (const relativePath of consumers) {
    const content = source(relativePath)
    assert.match(content, /DataPagination/)
    assert.doesNotMatch(content, /<el-pagination(?:\s|>)/)
  }
})

test('the shared pagination forwards Element Plus model updates through its controlled contract', () => {
  const content = source('common-frontend/basic/src/components/DataPagination.vue')

  assert.match(content, /@update:current-page="handleCurrentChange"/)
  assert.match(content, /@update:page-size="handlePageSizeChange"/)
  assert.doesNotMatch(content, /@current-change=/)
  assert.doesNotMatch(content, /@size-change=/)
  assert.match(content, /:size="size \|\| undefined"/)
  assert.doesNotMatch(content, /:small=/)
})

test('dynamic schema column logic has one non-map implementation', () => {
  assert.equal(existsSync(resolve(repositoryRoot, 'common-frontend/map/src/utils/dynamicSchemaColumns.js')), false)
  assert.equal(existsSync(resolve(repositoryRoot, 'common-frontend/basic/src/utils/dynamicSchemaColumns.js')), true)
})
