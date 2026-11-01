import { expect, test } from '@playwright/test'
import { createServer } from 'node:http'
import { readFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { observeFileGDBDownload } from './online/manager-filegdb-response.js'

const bytes = Buffer.from(Array.from({ length: 11593 }, (_, index) => index % 256))
const locator = 'addp://engine/2/path/authorization.gdb?type=file&item_id=1'
const downloadPath = '/api/v1/manager/downloads/file?'+new URLSearchParams({ locator })

// Real loopback HTTP, not route.fulfill: Chromium's consumed-blob response
// differs from an intercepted response. No development service or identity.
async function fixture(page, status, run) {
  const requests = []
  const server = createServer((request, response) => {
    if (request.url.startsWith('/api/')) {
      requests.push({ url: request.url, authorization: request.headers.authorization, cookie: request.headers.cookie })
      const authorized = request.headers.authorization === 'Bearer addp_at_fixture'
      const result = authorized ? status : 403
      const body = result === 200 ? bytes : Buffer.from(JSON.stringify({ error: 'denied' }))
      response.writeHead(result, { 'content-type': result === 200 ? 'application/zip' : 'application/json',
        'content-length': body.length, ...(result === 200 ? { 'content-disposition': 'attachment; filename=authorization.gdb.zip' } : {}) })
      response.end(body)
    } else {
      response.writeHead(200, { 'content-type': 'text/html' })
      response.end('<!doctype html><title>FileGDB response observation</title><script>window.downloadFetch=window.fetch.bind(window)</script>')
    }
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  let observer
  try {
    const origin = 'http://127.0.0.1:'+server.address().port
    observer = await observeFileGDBDownload(page, downloadPath)
    await page.goto(origin)
    await run({ observer, requests, origin })
  } finally {
    try { await observer?.dispose(page.mainFrame()) } finally { await new Promise(resolve => server.close(resolve)) }
  }
}

async function consume(page, save = true, path = downloadPath) {
  return page.evaluate(async ({ path, save }) => {
    const response = await window.downloadFetch(path, { headers: { Authorization: 'Bearer addp_at_fixture' },
      credentials: 'omit', cache: 'no-store', redirect: 'error' })
    const blob = await response.blob()
    if (save && response.ok) {
      const link = document.createElement('a')
      link.href = URL.createObjectURL(blob); link.download = 'authorization.gdb.zip'
      document.body.appendChild(link); link.click(); link.remove()
      setTimeout(() => URL.revokeObjectURL(link.href), 1000)
    }
    return { status: response.status, size: blob.size, bodyUsed: response.bodyUsed }
  }, { path, save })
}

test('same HTTP response clone equals saved binary bytes without an extra request', async ({ page }) => {
  await fixture(page, 200, async ({ observer, requests, origin }) => {
    const downloadPromise = page.waitForEvent('download')
    const consumed = await consume(page)
    const download = await downloadPromise
    try {
      expect(await download.failure()).toBeNull()
      const saved = readFileSync(await download.path())
      const copies = await observer.read(page.mainFrame())
      expect(copies).toHaveLength(1)
      expect(copies[0].url).toBe(origin+downloadPath)
      expect(copies[0].status).toBe(200)
      expect(copies[0].error).toBeUndefined()
      expect(Buffer.from(copies[0].bytes)).toEqual(saved)
      expect(saved).toEqual(bytes)
      expect(createHash('sha256').update(saved).digest('hex')).toBe(createHash('sha256').update(bytes).digest('hex'))
      expect(copies[0].headers['content-length']).toBe(String(saved.length))
      expect(copies[0].headers['content-type']).toBe('application/zip')
      expect(copies[0].headers['content-disposition']).toBe('attachment; filename=authorization.gdb.zip')
      expect(download.suggestedFilename()).toBe('authorization.gdb.zip')
      expect(consumed).toEqual({ status: 200, size: bytes.length, bodyUsed: true })
      expect(requests).toEqual([{ url: downloadPath, authorization: 'Bearer addp_at_fixture', cookie: undefined }])
    } finally { await download.delete() }
  })
})

for (const phase of ['ungranted', 'revoked', 'expired']) {
  test(`${phase} denial clone never saves a download or repeats a request`, async ({ page }) => {
    let downloads = 0
    page.on('download', () => downloads++)
    await fixture(page, 403, async ({ observer, requests }) => {
      expect((await consume(page)).status).toBe(403)
      const copies = await observer.read(page.mainFrame())
      expect(copies).toHaveLength(1)
      expect(copies[0].status).toBe(403)
      expect(JSON.parse(Buffer.from(copies[0].bytes).toString('utf8'))).toEqual({ error: 'denied' })
      expect(downloads).toBe(0)
      expect(requests).toEqual([{ url: downloadPath, authorization: 'Bearer addp_at_fixture', cookie: undefined }])
    })
  })
}

test('observer retains duplicate attempts and ignores a different target', async ({ page }) => {
  await fixture(page, 200, async ({ observer, requests }) => {
    await consume(page, false, downloadPath+'&other=1')
    expect(await observer.read(page.mainFrame())).toEqual([])
    await consume(page, false)
    await consume(page, false)
    expect(await observer.read(page.mainFrame())).toHaveLength(2)
    expect(requests).toHaveLength(3)
  })
})

test('module iframe captures its own bound fetch without a host response', async ({ page }) => {
  await fixture(page, 200, async ({ observer, requests, origin }) => {
    await page.evaluate(origin => new Promise(resolve => {
      const iframe = document.createElement('iframe')
      iframe.onload = resolve; iframe.src = origin+'/module-ui/manager/'
      document.body.appendChild(iframe)
    }), origin)
    const frame = page.frames().find(candidate => candidate.parentFrame())
    expect(await consume(frame, false)).toEqual({ status: 200, size: bytes.length, bodyUsed: true })
    const copies = await observer.read(frame)
    expect(copies).toHaveLength(1)
    expect(Buffer.from(copies[0].bytes)).toEqual(bytes)
    expect(await observer.read(page.mainFrame())).toEqual([])
    expect(requests).toHaveLength(1)
    await observer.dispose(frame)
  })
})

test('copy failure is explicit and leaves original response consumption intact', async ({ page }) => {
  await fixture(page, 200, async ({ observer, requests }) => {
    await page.evaluate(() => { Response.prototype.clone = () => { throw new Error('copy unavailable') } })
    expect(await consume(page, false)).toEqual({ status: 200, size: bytes.length, bodyUsed: true })
    expect(await observer.read(page.mainFrame())).toEqual([{ error: 'response_copy_failed' }])
    expect(requests).toHaveLength(1)
  })
})
