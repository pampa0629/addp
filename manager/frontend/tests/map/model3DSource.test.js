import { describe, expect, it, vi } from 'vitest'
vi.mock('@addp/common-frontend', () => ({ saveBlobDownload: vi.fn() }))
import { saveBlobDownload } from '@addp/common-frontend'
import { downloadModel3DSource, modelDownloadFileName, isManagedModel3DGLBURL, fetchModel3DSource, isModel3DSourceURL } from '@/utils/model3DSource'

const origin = 'https://addp.example'
const url = '/api/v1/manager/model_3d_glb/41/content?version=execution-1'

describe('controlled Manager model source', () => {
  it('limits authenticated fetch to the exact same-origin Manager endpoint', async () => {
    expect(isManagedModel3DGLBURL(url, origin)).toBe(true)
    expect(isManagedModel3DGLBURL(origin + url, origin)).toBe(true)
    const fetch = vi.fn()
    for (const target of [
      'https://external.example' + url, '//external.example' + url,
      '/api/v1/manager/storage-stream?locator=external',
      '/api/v1/manager/model_3d_glb/41/content/extra',
      '/api/v1/manager/model_3d_glb/041/content',
      'https://user:password@addp.example' + url,
      'data:model/gltf-binary,bytes'
    ]) {
      expect(isManagedModel3DGLBURL(target, origin)).toBe(false)
      await expect(fetchModel3DSource(target, fetch, { origin })).rejects.toThrow()
    }
    expect(fetch).not.toHaveBeenCalled()
  })

  it('fetches current User bytes without cookies, redirects or cached authorization', async () => {
    const bytes = new Uint8Array([103, 108, 84, 70]).buffer
    const fetch = vi.fn().mockResolvedValue({ ok: true, arrayBuffer: async () => bytes })
    expect(await fetchModel3DSource(url, fetch, { origin })).toBe(bytes)
    expect(fetch).toHaveBeenCalledWith(origin + url, expect.objectContaining({ credentials: 'omit', redirect: 'error', cache: 'no-store' }))
  })

  it('does not parse content after rejection or cancellation', async () => {
    const denied = vi.fn().mockResolvedValue({ ok: false, status: 403, json: async () => ({ error: 'denied' }) })
    await expect(fetchModel3DSource(url, denied, { origin })).rejects.toMatchObject({ status: 403 })
    const controller = new AbortController()
    const cancelled = vi.fn().mockResolvedValue({ ok: true, arrayBuffer: async () => { controller.abort(); return new ArrayBuffer(0) } })
    await expect(fetchModel3DSource(url, cancelled, { origin, signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' })
  })
})

const rawSource = '/api/v1/manager/storage-stream?locator=addp%3A%2F%2Fengine%2F9%2Fpath%2Fmodel.stl%3Ftype%3Dfile%26item_id%3D1&storage_ref=model.stl'
it('uses current identity for raw model bytes and rejects ticket or external source URLs', async () => {
  const bytes = new ArrayBuffer(84)
  const fetch = vi.fn().mockResolvedValue({ ok: true, arrayBuffer: async () => bytes })
  expect(isModel3DSourceURL(rawSource, origin)).toBe(true)
  expect(await fetchModel3DSource(rawSource, fetch, { origin })).toBe(bytes)
  expect(fetch).toHaveBeenCalledWith(origin + rawSource, expect.objectContaining({ credentials: 'omit', redirect: 'error', cache: 'no-store' }))
  for (const bad of [rawSource + '&ticket=old', 'https://outside.invalid'+rawSource, '/other/model.stl']) {
    await expect(fetchModel3DSource(bad, fetch, { origin })).rejects.toThrow()
  }
  expect(fetch).toHaveBeenCalledTimes(1)
})


const downloadURL = '/api/v1/manager/downloads/file?locator=addp%3A%2F%2Fengine%2F9%2Fpath%2Fmodel.stl'
it('keeps canonical attachment names including UTF-8, spaces, percent and quoted punctuation', () => {
  for (const [header, name] of [
    ['attachment; filename=model.stl', 'model.stl'],
    ['attachment; filename=" TEST.PNG "', ' TEST.PNG '],
    ["attachment; filename*=utf-8''%20%E6%A8%A1%E5%9E%8B%20%25.stl.zip%20", ' 模型 %.stl.zip '],
    ['attachment; filename="quoted\\"name.stl"', 'quoted"name.stl']
  ]) expect(modelDownloadFileName(header)).toBe(name)
  for (const header of ['', 'inline; filename=model.stl', 'attachment; filename=..',
    'attachment; filename="../model.stl"', 'attachment; filename="a\\\\b.stl"',
    "attachment; filename*=iso-8859-1''name.stl", "attachment; filename*=UTF-8''%FF.stl",
    "attachment; filename*=UTF-8''%0Amodel.stl", 'attachment; filename=a; filename=b']) {
    expect(() => modelDownloadFileName(header)).toThrow()
  }
})

it('saves the complete authenticated model source exactly once with its server name', async () => {
  vi.mocked(saveBlobDownload).mockClear()
  const blob = new Blob(['source ZIP bytes'])
  const fetch = vi.fn().mockResolvedValue({ ok: true, status: 200,
    headers: new Headers({ 'Content-Disposition': 'attachment; filename=" original.stl.zip "' }), blob: async () => blob })
  await downloadModel3DSource(downloadURL, fetch, { origin, language: 'zh-cn' })
  expect(fetch).toHaveBeenCalledWith(origin + downloadURL, expect.objectContaining({ method: 'GET', credentials: 'omit', redirect: 'error', cache: 'no-store', headers: { 'Accept-Language': 'zh-cn' } }))
  expect(saveBlobDownload).toHaveBeenCalledExactlyOnceWith(blob, ' original.stl.zip ')
})

it('never downloads a denied, partial, cancelled, malformed or external model response', async () => {
  vi.mocked(saveBlobDownload).mockClear()
  const fetch = vi.fn()
  for (const target of [downloadURL + '&ticket=secret', downloadURL + '&locator=duplicate',
    'https://other.invalid' + downloadURL, downloadURL + '#fragment', '/api/v1/develop/exports/1/file']) {
    await expect(downloadModel3DSource(target, fetch, { origin })).rejects.toThrow()
  }
  expect(fetch).not.toHaveBeenCalled()
  for (const status of [401, 403, 503]) {
    fetch.mockResolvedValue({ ok: false, status, json: async () => ({ error: 'current user denied' }) })
    await expect(downloadModel3DSource(downloadURL, fetch, { origin })).rejects.toMatchObject({ status })
  }
  fetch.mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'Content-Disposition': 'attachment; filename=model.stl' }), blob: async () => { throw new Error('body interrupted') } })
  await expect(downloadModel3DSource(downloadURL, fetch, { origin })).rejects.toThrow('body interrupted')
  fetch.mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'Content-Disposition': 'attachment; filename=model.stl' }), blob: async () => new Blob([]) })
  await expect(downloadModel3DSource(downloadURL, fetch, { origin })).rejects.toThrow()
  fetch.mockResolvedValue({ ok: true, status: 206 })
  await expect(downloadModel3DSource(downloadURL, fetch, { origin })).rejects.toThrow()
  const controller = new AbortController()
  fetch.mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'Content-Disposition': 'attachment; filename=model.stl' }), blob: async () => { controller.abort(); return new Blob(['bytes']) } })
  await expect(downloadModel3DSource(downloadURL, fetch, { origin, signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' })
  expect(saveBlobDownload).not.toHaveBeenCalled()
})
