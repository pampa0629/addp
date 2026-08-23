import test from 'node:test'
import assert from 'node:assert/strict'

import {
  isTransientRequestError,
  withTransientRetry
} from '../../../common-frontend/basic/src/utils/transientRequest.js'

test('引擎发现遇到 Gateway 503 后自动重试并返回恢复后的结果', async () => {
  let calls = 0
  const waits = []
  const result = await withTransientRetry(async () => {
    calls += 1
    if (calls < 3) {
      throw { response: { status: 503 } }
    }
    return [{ id: 2, connection_status: 'online' }]
  }, {
    delays: [10, 20],
    wait: async delay => waits.push(delay)
  })

  assert.equal(calls, 3)
  assert.deepEqual(waits, [10, 20])
  assert.deepEqual(result, [{ id: 2, connection_status: 'online' }])
})

test('非暂态业务错误不会重试', async () => {
  let calls = 0
  await assert.rejects(
    withTransientRetry(async () => {
      calls += 1
      throw { response: { status: 422 } }
    }, {
      delays: [0, 0],
      wait: async () => {}
    }),
    error => error?.response?.status === 422
  )
  assert.equal(calls, 1)
})

test('仅把网关暂态状态和网络中断识别为可重试错误', () => {
  assert.equal(isTransientRequestError({ response: { status: 502 } }), true)
  assert.equal(isTransientRequestError({ response: { status: 503 } }), true)
  assert.equal(isTransientRequestError({ response: { status: 504 } }), true)
  assert.equal(isTransientRequestError({ code: 'ERR_NETWORK' }), true)
  assert.equal(isTransientRequestError({ response: { status: 401 } }), false)
})
