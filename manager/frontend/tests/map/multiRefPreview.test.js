import { describe, expect, it } from 'vitest'
import {
  combinedMultiRefValue,
  multiPreviewRefs,
  multiRefPreviewOptions
} from '../../src/utils/multiRefPreview'

const t = (key) => ({
  'containerPreview.combinedPreview': '组合预览'
}[key] || key)

describe('multiRefPreview', () => {
  it('builds preview options from item attribute refs', () => {
    const previewData = {
      object: {
        attributes: {
          item: {
            layout: 'multi',
            refs: [
              { path: 'addp/image/srtm_40_01.tif', role: 'main', primary: true, required: true, extension: '.tif' },
              { path: 'addp/image/srtm_40_01.tfw', role: 'world_file', extension: '.tfw' }
            ]
          }
        }
      }
    }

    expect(multiPreviewRefs(previewData)).toMatchObject([
      { path: 'addp/image/srtm_40_01.tif', role: 'main', primary: true, required: true, extension: '.tif' },
      { path: 'addp/image/srtm_40_01.tfw', role: 'world_file', extension: '.tfw' }
    ])
    expect(multiRefPreviewOptions(previewData, t)).toEqual([
      { key: combinedMultiRefValue, path: combinedMultiRefValue, label: '组合预览' },
      {
        key: 'main',
        path: 'addp/image/srtm_40_01.tif',
        label: 'main · srtm_40_01.tif',
        role: 'main',
        primary: true,
        required: true,
        extension: '.tif'
      },
      {
        key: 'world_file',
        path: 'addp/image/srtm_40_01.tfw',
        label: 'world_file · srtm_40_01.tfw',
        role: 'world_file',
        primary: false,
        required: false,
        extension: '.tfw'
      }
    ])
  })

  it('deduplicates refs from content metadata and item attributes', () => {
    const previewData = {
      object: {
        content: {
          metadata: {
            refs: [
              { path: 'dataset/main.shp', role: 'main' },
              { path: 'dataset/main.dbf', role: 'attributes' }
            ]
          }
        },
        attributes: {
          item: {
            refs: [
              { path: 'dataset/main.shp', role: 'main' },
              { path: 'dataset/main.shx', role: 'index' }
            ]
          }
        }
      }
    }

    expect(multiPreviewRefs(previewData).map(ref => ref.path)).toEqual([
      'dataset/main.shp',
      'dataset/main.dbf',
      'dataset/main.shx'
    ])
  })

  it('returns no options when refs are absent', () => {
    expect(multiRefPreviewOptions({ object: { attributes: { item: { layout: 'single' } } } }, t)).toEqual([])
  })
})
