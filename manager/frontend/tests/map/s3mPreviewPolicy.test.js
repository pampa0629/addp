import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import {
  isCompleteWebpMipChain,
  parseWebpMipPayload
} from '../../src/lib/supermap-s3m/S3MTiles/MaterialPass.js'
import ContentState from '../../src/lib/supermap-s3m/S3MTiles/Enum/ContentState.js'
import { readDracoAttributeInfo } from '../../src/lib/supermap-s3m/S3MParser/ParseDraco.js'
import {
  mapStandardTextureCompression,
  standardTextureInternalFormat
} from '../../src/lib/supermap-s3m/S3MParser/S3MTextureFormat.js'

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
const s3mParserSource = readFileSync(
  new URL('../../src/lib/supermap-s3m/S3MParser/S3ModelParser.js', import.meta.url),
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

  it('passes the managed S3M artifact encoding instead of hardcoding legacy tiles', () => {
    expect(previewPanelSource).toContain('selected?.manifest_encoding')
    expect(previewPanelSource).toContain('selected?.tile_extension')
    expect(previewPanelSource).toContain('selected?.texture_compression')
    expect(previewPanelSource).toContain('selected?.geometry_compression')
    expect(previewPanelSource).toContain('selected?.s3m_version')
    expect(previewPanelSource).not.toContain("manifest_encoding: isS3M ? 'xml'")
    expect(previewPanelSource).not.toContain("tile_extension: isS3M ? '.s3m'")
  })

  it('uses the managed renderer that retains legacy .s3m parsing', () => {
    expect(s3mPreviewSource).toContain('@/lib/supermap-s3m/S3MTiles/S3MTilesLayer.js')
    expect(s3mPreviewSource).not.toContain("import('@dfsj/s3m')")
    expect(s3mTileSource).toContain("import S3ModelOldParser from '../S3MParser/S3ModelOldParser.js'")
    expect(s3mTileSource).toContain("tile.fileExtension === 's3m'")
  })

  it('waits for the Draco decoder before parsing S3MB content', () => {
    expect(s3mParserSource).toContain('S3ModelParser.readyPromise')
    expect(s3mParserSource).toContain('await S3ModelParser.readyPromise')
    expect(s3mParserSource).toContain('dracoLib = compiledModule')
    expect(s3mParserSource).toContain('resolve()')
    expect(s3mParserSource).not.toContain('return dracoDecoderModule')
    expect(s3mParserSource).not.toContain('if(dracoLib) return')
  })

  it('uses the S3M major version for 3.01 Draco binary layout checks', () => {
    expect(s3mParserSource).toContain('Math.trunc(version) === 3')
    expect(s3mParserSource).toContain('version === 3.01')
  })

  it('reads the S3M 3.01 direct custom Draco attribute ID without shifting material data', () => {
    const words = new Int32Array([
      3988,
      0,
      0,
      -1,
      -1,
      1,
      1,
      1,
      2,
      1
    ])
    const view = new DataView(words.buffer)
    const result = readDracoAttributeInfo(view, 0, 3.01)

    expect(result.attributes).toEqual({
      posUniqueID: 0,
      normalUniqueID: -1,
      colorUniqueID: -1,
      secondColorUniqueID: 1,
      texCoordUniqueIDs: [1],
      vertexAttrUniqueIDs: [2]
    })
    expect(result.bytesOffset).toBe(9 * Int32Array.BYTES_PER_ELEMENT)
    expect(view.getInt32(result.bytesOffset, true)).toBe(1)
  })

  it('consumes S3M 3.01 LOD metadata before optional pick data', () => {
    expect(s3mParserSource).toContain('parseS3M301LodMetadata')
    expect(s3mParserSource).toContain('result.lodProcessType')
    expect(s3mParserSource).toContain('if(version === 3)')
    expect(s3mParserSource).not.toContain('if(version >= 3){\n        nOptions = view.getUint32')
  })

  it('preserves the DXT subtype declared by S3M 3.01 textures', () => {
    expect(mapStandardTextureCompression(33776)).toBe(17)
    expect(mapStandardTextureCompression(33779)).toBe(21)
    expect(standardTextureInternalFormat(33776, 32849)).toBe(33776)
    expect(standardTextureInternalFormat(33779, 32849)).toBe(33779)
  })

  it('propagates asynchronous S3M content failures through the tile state', () => {
    expect(s3mTileSource).toContain('return contentReadyFunction(layer, that, arrayBuffer)')
    expect(s3mTileSource).toContain('.catch(function(error)')
    expect(s3mTileSource).toContain('contentFailedFunction(error)')
    expect(s3mTileSource).not.toContain('// contentFailedFunction(error)')
  })

  it('defines the loaded state consumed by the S3M scheduler', () => {
    expect(ContentState.LOADED).toBeTypeOf('number')
    expect(ContentState.LOADED).not.toBe(ContentState.PARSING)
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

  it('scopes shared scene camera state to the renderer and managed artifact version', () => {
    expect(previewPanelSource).toContain('const quickViewArtifactVersion = computed')
    expect(previewPanelSource).toContain('state.render_source !== quickViewRenderSource.value')
    expect(previewPanelSource).toContain('state.artifact_version !== artifactVersion')
    expect(previewPanelSource).toContain('render_source: quickViewRenderSource.value')
    expect(previewPanelSource).toContain('artifact_version: quickViewArtifactVersion.value')
  })

  it('contains the standalone Manager body so percentage-height previews cannot grow the page', () => {
    expect(layoutSource).toContain('<el-container class="body-container">')
    expect(layoutSource).toMatch(/\.body-container\s*\{[^}]*flex:\s*1[^}]*min-height:\s*0[^}]*overflow:\s*hidden/s)
    expect(layoutSource).toMatch(/\.main-content\s*\{[^}]*min-height:\s*0/s)
  })
})
