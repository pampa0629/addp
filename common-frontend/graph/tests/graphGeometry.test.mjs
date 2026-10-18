import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('本体、结构图和查询结果共享唯一的边类型选择', () => {
  for (const file of ['OntologyView.vue', 'GraphResultView.vue', 'GraphSchemaView.vue']) {
    const source = readFileSync(new URL(`../src/${file}`, import.meta.url), 'utf8')
    assert.match(source, /import \{ graphEdgeType \} from '\.\/graphGeometry\.js'/)
    assert.match(source, /type: graphEdgeType\(/)
  }
})
