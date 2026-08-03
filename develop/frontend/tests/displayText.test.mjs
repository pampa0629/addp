import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { hasDistinctText } from '../src/utils/displayText.js'

assert.equal(hasDistinctText('数据加载', '数据加载', 'load'), false)
assert.equal(hasDistinctText('LOAD', 'load'), false)
assert.equal(hasDistinctText('读取空间数据', '数据加载', 'load'), true)
assert.equal(hasDistinctText('', '数据加载'), false)

const queryEditor = readFileSync(resolve(import.meta.dirname, '../src/views/QueryEditor.vue'), 'utf8')
assert.equal(
  /SELECT\s+1\b/i.test(queryEditor),
  false,
  'QueryEditor must not replace executable engine samples with SELECT 1'
)

console.log('displayText tests passed')
