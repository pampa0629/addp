import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import {
  isCompleteWebpMipChain,
  parseWebpMipPayload
} from '../../src/lib/supermap-s3m/S3MTiles/MaterialPass.js'

const previewPanelSource = readFileSync(
  new URL('../../src/components/explorer/PreviewPanel.vue', import.meta.url),
  'utf8'
)
const s3mPreviewSource = readFileSync(
  new URL('../../src/components/explorer/S3MPreview.vue', import.meta.url),
  'utf8'
)
const materialPassSource = readFileSync(
  new URL('../../src/lib/supermap-s3m/S3MTiles/MaterialPass.js', import.meta.url),
  'utf8'
)
const layoutSource = readFileSync(
  new URL('../../src/components/Layout.vue', import.meta.url),
  'utf8'
)
const s3mTileSource = readFileSync(
  new URL('../../src/lib/supermap-s3m/S3MTiles/S3MTile.js', import.meta.url),
  'utf8'
)

describe('S3M preview policy', () => {
  it('keeps the source item format beside the title and the quick-view format with the actions', () => {
    const headerStart = previewPanelSource.indexOf('<div class="header-left">')
    const headerEnd = previewPanelSource.indexOf('<div class="panel-actions">', headerStart)
    const header = previewPanelSource.slice(headerStart, headerEnd)
    const actionsEnd = previewPanelSource.indexOf('</div>', headerEnd)
    const actions = previewPanelSource.slice(headerEnd, actionsEnd)

    expect(header).toContain('current-item-format')
    expect(header).not.toContain('model3d-tiles-format-switcher')
    expect(actions).toContain('model3d-tiles-format-switcher')
    expect(previewPanelSource).toContain('s3mTileQuickView')
    expect(previewPanelSource.match(/class="model3d-tiles-format-switcher"/g)).toHaveLength(1)
    expect(previewPanelSource).not.toContain('currentQuickViewFormat')
  })

  it('does not require S3TC for WebP-backed S3M results', () => {
    expect(s3mPreviewSource).not.toContain('WEBGL_compressed_texture_s3tc')
    expect(s3mPreviewSource).not.toContain('s3mS3TCRequired')
  })

  it('uses the managed renderer that retains legacy .s3m parsing', () => {
    expect(s3mPreviewSource).toContain('@/lib/supermap-s3m/S3MTiles/S3MTilesLayer.js')
    expect(s3mPreviewSource).not.toContain("import('@dfsj/s3m')")
    expect(s3mTileSource).toContain("import S3ModelOldParser from '../S3MParser/S3ModelOldParser.js'")
    expect(s3mTileSource).toContain("tile.fileExtension === 's3m'")
  })

  it('uploads decoded WebP mip images directly while restoring WebGL state', () => {
    expect(materialPassSource).toContain('gl.texImage2D')
    expect(materialPassSource).toContain('gl.getParameter(gl.ACTIVE_TEXTURE)')
    expect(materialPassSource).toContain('gl.getParameter(gl.TEXTURE_BINDING_2D)')
    expect(materialPassSource).toContain('decodedImages[level]')
    expect(materialPassSource).not.toContain('OffscreenCanvas')
    expect(materialPassSource).not.toContain('getImageData')
    expect(materialPassSource).not.toContain('source.mipLevels')
    expect(materialPassSource).not.toContain('texture.generateMipmap')
  })

  it('parses every length-prefixed WebP mip level and validates rectangular chains', () => {
    const chunks = [
      new Uint8Array([1, 2, 3]),
      new Uint8Array([4, 5]),
      new Uint8Array([6])
    ]
    const byteLength = chunks.reduce((total, chunk) => total + 4 + chunk.byteLength, 0)
    const payload = new Uint8Array(byteLength)
    const view = new DataView(payload.buffer)
    let offset = 0
    for (const chunk of chunks) {
      view.setUint32(offset, chunk.byteLength, true)
      offset += 4
      payload.set(chunk, offset)
      offset += chunk.byteLength
    }

    expect(parseWebpMipPayload(payload).map(level => Array.from(level))).toEqual([
      [1, 2, 3],
      [4, 5],
      [6]
    ])
    expect(isCompleteWebpMipChain([
      { width: 256, height: 512 },
      { width: 128, height: 256 },
      { width: 64, height: 128 },
      { width: 32, height: 64 },
      { width: 16, height: 32 },
      { width: 8, height: 16 },
      { width: 4, height: 8 },
      { width: 2, height: 4 },
      { width: 1, height: 2 },
      { width: 1, height: 1 }
    ])).toBe(true)
    expect(isCompleteWebpMipChain([
      { width: 256, height: 512 },
      { width: 128, height: 128 }
    ])).toBe(false)
  })

  it('removes the S3M primitive before destroying the viewer without mutating Vue DOM', () => {
    expect(s3mPreviewSource).toContain('currentViewer.scene.primitives.remove(currentLayer)')
    expect(s3mPreviewSource).not.toContain('layer.destroy()')
    expect(s3mPreviewSource).not.toContain('replaceChildren()')
  })

  it('contains the standalone Manager body so percentage-height previews cannot grow the page', () => {
    expect(layoutSource).toContain('<el-container class="body-container">')
    expect(layoutSource).toMatch(/\.body-container\s*\{[^}]*flex:\s*1[^}]*min-height:\s*0[^}]*overflow:\s*hidden/s)
    expect(layoutSource).toMatch(/\.main-content\s*\{[^}]*min-height:\s*0/s)
  })
})
