import assert from 'node:assert/strict'
import test from 'node:test'

import {
  isResourceTreeSearchReady,
  minimumResourceTreeSearchLength
} from '../src/utils/resourceTreeSearch.mjs'

test('资源树搜索遵守服务端最小关键词长度', () => {
  assert.equal(minimumResourceTreeSearchLength, 2)
  assert.equal(isResourceTreeSearchReady(''), false)
  assert.equal(isResourceTreeSearchReady('a'), false)
  assert.equal(isResourceTreeSearchReady('点'), false)
  assert.equal(isResourceTreeSearchReady('ab'), true)
  assert.equal(isResourceTreeSearchReady('图层'), true)
})
