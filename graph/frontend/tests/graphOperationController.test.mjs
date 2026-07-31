import assert from 'node:assert/strict'
import { test } from 'vitest'
import { createLatestOperationController } from '../src/utils/graphOperationController.js'

function deferred() {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

test('只有最后启动的探索操作可以写入状态', async () => {
  const controller = createLatestOperationController()
  const overview = deferred()
  const search = deferred()
  const writes = []
  let firstSignal

  const first = controller.execute(
    'overview',
    signal => {
      firstSignal = signal
      return overview.promise
    },
    { onSuccess: value => writes.push(value) },
  )
  const second = controller.execute(
    'search',
    () => search.promise,
    { onSuccess: value => writes.push(value) },
  )

  assert.equal(firstSignal.aborted, true)
  overview.resolve('stale overview')
  search.resolve('latest search')
  await Promise.all([first, second])
  assert.deepEqual(writes, ['latest search'])
})

test('取消控制器后不再执行成功或完成回调', async () => {
  const controller = createLatestOperationController()
  const request = deferred()
  const callbacks = []
  const pending = controller.execute('expand', () => request.promise, {
    onSuccess: () => callbacks.push('success'),
    onFinish: () => callbacks.push('finish'),
  })

  controller.cancel()
  request.resolve('ignored')
  await pending
  assert.deepEqual(callbacks, [])
})
