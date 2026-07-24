import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { createConcurrencyLimiter } from '../src/utils/concurrency.js'

const flushTasks = () => new Promise(resolve => setImmediate(resolve))

test('concurrency limiter caps active tasks and preserves queue order', async () => {
  const limit = createConcurrencyLimiter(2)
  const releases = new Map()
  const started = []
  let active = 0
  let maxActive = 0

  const tasks = Array.from({ length: 5 }, (_, index) => limit(async () => {
    started.push(index)
    active += 1
    maxActive = Math.max(maxActive, active)
    await new Promise(resolve => releases.set(index, resolve))
    active -= 1
    return index
  }))

  await flushTasks()
  assert.deepEqual(started, [0, 1])
  assert.equal(maxActive, 2)

  releases.get(1)()
  await flushTasks()
  assert.deepEqual(started, [0, 1, 2])

  releases.get(0)()
  await flushTasks()
  assert.deepEqual(started, [0, 1, 2, 3])

  releases.get(2)()
  releases.get(3)()
  await flushTasks()
  assert.deepEqual(started, [0, 1, 2, 3, 4])

  releases.get(4)()
  assert.deepEqual(await Promise.all(tasks), [0, 1, 2, 3, 4])
})

test('concurrency limiter releases a slot after task failure', async () => {
  const limit = createConcurrencyLimiter(1)
  const started = []

  const failed = limit(async () => {
    started.push('failed')
    throw new Error('expected failure')
  })
  const succeeded = limit(async () => {
    started.push('succeeded')
    return 'ok'
  })

  await assert.rejects(failed, /expected failure/)
  assert.equal(await succeeded, 'ok')
  assert.deepEqual(started, ['failed', 'succeeded'])
})

test('concurrency limiter rejects invalid limits', () => {
  assert.throws(() => createConcurrencyLimiter(0), /positive integer/)
})

test('task panel loads providers concurrently through one request limiter', async () => {
  const source = await readFile(new URL('../src/components/TaskPanel.vue', import.meta.url), 'utf8')

  assert.match(source, /createConcurrencyLimiter\(TASK_REQUEST_CONCURRENCY\)/)
  assert.match(source, /treeData\.value = providerStates\.map\(buildProviderNode\)/)
  assert.match(source, /Promise\.all\(providerStates\.map/)
  assert.match(source, /scheduleTaskRequest\(\(\) => \(/)
  assert.match(source, /updateTaskTypeNode\(identifier, taskType, tasks, false\)/)
  assert.doesNotMatch(source, /for \(const provider of providers\)/)
})
