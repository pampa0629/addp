import { describe, expect, it } from 'vitest'
import { resolveCADTileURL } from '../../src/utils/cadPreviewURL.js'

describe('cadPreviewURL', () => {
  it('resolves an absolute-path tile template from a relative manifest URL', () => {
    expect(resolveCADTileURL(
      '/api/v1/manager/cad-previews/3/tiles/{z}/{x}/{y}',
      2,
      1,
      3,
      '/api/v1/manager/cad-previews/3/manifest',
      'http://localhost:5174'
    )).toBe('http://localhost:5174/api/v1/manager/cad-previews/3/tiles/2/1/3')
  })

  it('resolves a manifest-relative tile template', () => {
    expect(resolveCADTileURL(
      'model-space/{z}/{x}/{y}.webp',
      0,
      0,
      0,
      'http://localhost:5174/previews/3/manifest.json'
    )).toBe('http://localhost:5174/previews/3/model-space/0/0/0.webp')
  })
})
