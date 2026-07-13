import { describe, expect, it } from 'vitest'
import { isCADPreviewSource } from '../../src/utils/cadPreviewSource.js'

describe('cadPreviewSource', () => {
  it('recognizes a scanned single DWG item as a CAD preview source', () => {
    expect(isCADPreviewSource({
      object: {
        attributes: {
          item: {
            data_type: 'cad',
            format: 'dwg',
            layout: 'single'
          }
        }
      }
    }, {
      path: 'cad/example_r14.dwg'
    })).toBe(true)
  })

  it('recognizes an unscanned DWG path so capability loading can discover support', () => {
    expect(isCADPreviewSource({}, {
      path: 'cad/example_r14.dwg'
    })).toBe(true)
  })

  it('recognizes DXF and rejects non-single CAD items', () => {
    expect(isCADPreviewSource({
      object: {
        attributes: {
          item: { data_type: 'cad', format: 'dxf', layout: 'single' }
        }
      }
    }, { path: 'cad/example.dxf' })).toBe(true)

    expect(isCADPreviewSource({}, { path: 'cad/example.dxf' })).toBe(true)

    expect(isCADPreviewSource({
      object: {
        attributes: {
          item: { data_type: 'cad', format: 'dwg', layout: 'multi' }
        }
      }
    }, { path: 'cad/drawing.dwg' })).toBe(false)
  })
})
