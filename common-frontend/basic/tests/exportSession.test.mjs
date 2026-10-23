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

test('role export download uses the authenticated client and refuses external or ticket URLs', async () => {
  const { downloadAuthenticatedExport } = await import('../src/utils/exportSession.js')
  const savedWindow = globalThis.window
  const savedDocument = globalThis.document
  const clicked = []
  globalThis.window = { location: { origin: 'https://addp.example' } }
  globalThis.document = {
    createElement() { return { click() { clicked.push({ href: this.href, download: this.download }) } } },
    body: { appendChild() {}, removeChild() {} }
  }
  try {
    let calls = 0
    const client = { async get(url, options) {
      calls++
      assert.equal(url, 'https://addp.example/api/v1/develop/exports/7/file')
      assert.equal(options.responseType, 'blob')
      assert.equal('Authorization' in options, false)
      return new Blob(['protected csv'], { type: 'text/csv' })
    } }
    await downloadAuthenticatedExport(client, { download_url: '/api/v1/develop/exports/7/file', file_name: 'protected.csv' })
    assert.equal(calls, 1)
    assert.equal(clicked.length, 1)
    assert.match(clicked[0].href, /^blob:/)
    assert.equal(clicked[0].download, 'protected.csv')
    for (const download_url of ['https://elsewhere.example/api/v1/develop/exports/7/file', '/api/v1/develop/exports/7/file?ticket=secret', '/api/v1/develop/exports/7/file#secret', '/api/v1/develop/exports/07/file']) {
      await assert.rejects(downloadAuthenticatedExport(client, { download_url }), /Invalid export download URL/)
    }
    assert.equal(calls, 1)
  } finally {
    globalThis.window = savedWindow
    globalThis.document = savedDocument
  }
})

test('authenticated export preserves a bounded localized protection error', async () => {
  const { downloadAuthenticatedExport } = await import('../src/utils/exportSession.js')
  const savedWindow = globalThis.window
  globalThis.window = { location: { origin: 'https://addp.example' } }
  const error = { response: { data: new Blob([JSON.stringify({ error: '角色资格已失效', error_code: 'query_protection_roles_unavailable' })], { type: 'application/json' }) } }
  try {
    await assert.rejects(downloadAuthenticatedExport({ get: async () => { throw error } }, { download_url: '/api/v1/develop/exports/7/file' }), result => result === error && result.response.data.error_code === 'query_protection_roles_unavailable')
  } finally { globalThis.window = savedWindow }
})
