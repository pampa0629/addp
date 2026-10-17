import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('查询结果与结构图的所有自动适应入口归属同一共享实现', () => {
  for (const file of ['GraphResultView.vue', 'GraphSchemaView.vue']) {
    const source = readFileSync(new URL(`../src/${file}`, import.meta.url), 'utf8')
    assert.match(source, /import \{ fitGraphToViewport \} from '\.\/graphViewport\.js'/)
    assert.match(source, /on\('afterlayout', fitView\)/)
    assert.doesNotMatch(source, /graphInstance\??\.fitView\(/)
  }
})
