import assert from 'node:assert/strict'
import test from 'node:test'

import { waitForExportSession } from '../src/utils/exportSession.js'

test('waitForExportSession returns the completed session', async () => {
  const states = [
    { status: 'pending' },
    { status: 'running' },
    { status: 'success', download_url: '/exports/1/file', file_name: 'data.csv' }
  ]
  const session = await waitForExportSession(async () => states.shift(), 1, { intervalMs: 1 })
  assert.equal(session.download_url, '/exports/1/file')
})

test('waitForExportSession exposes the backend failure', async () => {
  await assert.rejects(
    waitForExportSession(async () => ({ status: 'failed', error_message: 'writer failed' }), 1, { intervalMs: 1 }),
    /writer failed/
  )
})

test('waitForExportSession stops polling when cancelled', async () => {
  const controller = new AbortController()
  let calls = 0
  const polling = waitForExportSession(async () => {
    calls += 1
    return { status: 'running' }
  }, 1, { intervalMs: 100, signal: controller.signal })

  controller.abort()

  await assert.rejects(polling, error => error?.name === 'AbortError')
  assert.equal(calls, 1)
})
