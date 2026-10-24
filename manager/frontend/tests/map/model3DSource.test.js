import { describe, expect, it, vi } from 'vitest'
import { isManagedModel3DGLBURL, fetchModel3DSource, isModel3DSourceURL } from '@/utils/model3DSource'

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
