import { describe, expect, it, vi } from 'vitest'
import { isManagedModel3DGLBURL, loadManagedModel3DGLB } from '@/utils/model3DGLBSource'

const origin = 'https://addp.example'
const url = '/api/v1/manager/model_3d_glb/41/content?version=execution-1'

describe('managed model GLB source', () => {
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
      await expect(loadManagedModel3DGLB({}, target, fetch, { origin })).rejects.toThrow()
    }
    expect(fetch).not.toHaveBeenCalled()
  })

  it('fetches current User bytes without cookies or redirects and parses locally', async () => {
    const bytes = new Uint8Array([103, 108, 84, 70]).buffer
    const parsed = { scene: {} }
    const loader = { parseAsync: vi.fn().mockResolvedValue(parsed) }
    const fetch = vi.fn().mockResolvedValue({ ok: true, arrayBuffer: async () => bytes })
    expect(await loadManagedModel3DGLB(loader, url, fetch, { origin })).toBe(parsed)
    expect(fetch).toHaveBeenCalledWith(origin + url, expect.objectContaining({ credentials: 'omit', redirect: 'error', cache: 'no-store' }))
    expect(loader.parseAsync).toHaveBeenCalledWith(bytes, '')
  })

  it('does not parse content after rejection or cancellation', async () => {
    const loader = { parseAsync: vi.fn() }
    const denied = vi.fn().mockResolvedValue({ ok: false, status: 403, json: async () => ({ error: 'denied' }) })
    await expect(loadManagedModel3DGLB(loader, url, denied, { origin })).rejects.toMatchObject({ status: 403 })
    const controller = new AbortController()
    const cancelled = vi.fn().mockResolvedValue({ ok: true, arrayBuffer: async () => { controller.abort(); return new ArrayBuffer(0) } })
    await expect(loadManagedModel3DGLB(loader, url, cancelled, { origin, signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' })
    expect(loader.parseAsync).not.toHaveBeenCalled()
  })
})
