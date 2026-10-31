import { expect, it, vi } from 'vitest'
vi.mock('@addp/common-frontend', () => ({ saveBlobDownload: vi.fn() }))
import { saveBlobDownload } from '@addp/common-frontend'
import { downloadStorageSource, storageDownloadFileName } from '@/utils/storageSource'
const origin = 'https://addp.example'

const downloadURL = '/api/v1/manager/downloads/file?locator=addp%3A%2F%2Fengine%2F9%2Fpath%2Fmodel.stl'
it('keeps canonical attachment names including UTF-8, spaces, percent and quoted punctuation', () => {
  for (const [header, name] of [
    ['attachment; filename=model.stl', 'model.stl'],
    ['attachment; filename=" TEST.PNG "', ' TEST.PNG '],
    ["attachment; filename*=utf-8''%20%E6%A8%A1%E5%9E%8B%20%25.stl.zip%20", ' 模型 %.stl.zip '],
    ['attachment; filename="quoted\\"name.stl"', 'quoted"name.stl']
  ]) expect(storageDownloadFileName(header)).toBe(name)
  for (const header of ['', 'inline; filename=model.stl', 'attachment; filename=..',
    'attachment; filename="../model.stl"', 'attachment; filename="a\\\\b.stl"',
    "attachment; filename*=iso-8859-1''name.stl", "attachment; filename*=UTF-8''%FF.stl",
    "attachment; filename*=UTF-8''%0Amodel.stl", 'attachment; filename=a; filename=b']) {
    expect(() => storageDownloadFileName(header)).toThrow()
  }
})

it('saves the complete authenticated storage source exactly once with its server name', async () => {
  vi.mocked(saveBlobDownload).mockClear()
  const blob = new Blob(['source ZIP bytes'])
  const fetch = vi.fn().mockResolvedValue({ ok: true, status: 200,
    headers: new Headers({ 'Content-Disposition': 'attachment; filename=" original.stl.zip "' }), blob: async () => blob })
  await downloadStorageSource(downloadURL, fetch, { origin, language: 'zh-cn' })
  expect(fetch).toHaveBeenCalledWith(origin + downloadURL, expect.objectContaining({ method: 'GET', credentials: 'omit', redirect: 'error', cache: 'no-store', headers: { 'Accept-Language': 'zh-cn' } }))
  expect(saveBlobDownload).toHaveBeenCalledExactlyOnceWith(blob, ' original.stl.zip ')
})

it('never downloads a denied, partial, cancelled, malformed or external storage response', async () => {
  vi.mocked(saveBlobDownload).mockClear()
  const fetch = vi.fn()
  for (const target of [downloadURL + '&ticket=secret', downloadURL + '&locator=duplicate',
    'https://other.invalid' + downloadURL, downloadURL + '#fragment', '/api/v1/develop/exports/1/file']) {
    await expect(downloadStorageSource(target, fetch, { origin })).rejects.toThrow()
  }
  expect(fetch).not.toHaveBeenCalled()
  for (const status of [401, 403, 409, 500, 503]) {
    fetch.mockResolvedValue({ ok: false, status, json: async () => ({ error: 'current user denied' }) })
    await expect(downloadStorageSource(downloadURL, fetch, { origin })).rejects.toMatchObject({ status })
  }
  fetch.mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'Content-Disposition': 'attachment; filename=model.stl' }), blob: async () => { throw new Error('body interrupted') } })
  await expect(downloadStorageSource(downloadURL, fetch, { origin })).rejects.toThrow('body interrupted')
  fetch.mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'Content-Disposition': 'attachment; filename=model.stl' }), blob: async () => new Blob([]) })
  await expect(downloadStorageSource(downloadURL, fetch, { origin })).rejects.toThrow()
  fetch.mockResolvedValue({ ok: true, status: 206 })
  await expect(downloadStorageSource(downloadURL, fetch, { origin })).rejects.toThrow()
  const controller = new AbortController()
  fetch.mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'Content-Disposition': 'attachment; filename=model.stl' }), blob: async () => { controller.abort(); return new Blob(['bytes']) } })
  await expect(downloadStorageSource(downloadURL, fetch, { origin, signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' })
  expect(saveBlobDownload).not.toHaveBeenCalled()
})
