import assert from 'node:assert/strict'
import { hasDistinctText } from '../src/utils/displayText.js'

assert.equal(hasDistinctText('数据加载', '数据加载', 'load'), false)
assert.equal(hasDistinctText('LOAD', 'load'), false)
assert.equal(hasDistinctText('读取空间数据', '数据加载', 'load'), true)
assert.equal(hasDistinctText('', '数据加载'), false)

console.log('displayText tests passed')
