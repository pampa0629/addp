import { describe, expect, it } from 'vitest'
import {
  buildTilesetSource,
  parseStorageStreamURL,
  resolveTileResourceURL,
  virtualTileURL
} from '@/utils/threeDTilesPreviewUrl'

const origin = 'http://localhost:5174'

describe('threeDTilesPreviewUrl', () => {
  it('builds virtual source for manager storage stream URL', () => {
    const url = '/api/v1/manager/storage-stream?engine_id=31&storage_ref=tiles/site/tileset.json'

    const source = buildTilesetSource(url, origin)

    expect(source).toEqual({
      rootURL: 'http://localhost:5174/__addp_3dtiles__/tiles/site/tileset.json',
      engineID: '31',
      storageRef: 'tiles/site/tileset.json',
      virtual: true
    })
  })

  it('resolves relative tile resources back to storage stream', () => {
    const source = {
      rootURL: 'http://localhost:5174/__addp_3dtiles__/tiles/site/tileset.json',
      engineID: '31',
      storageRef: 'tiles/site/tileset.json',
      virtual: true
    }

    const url = resolveTileResourceURL('http://localhost:5174/__addp_3dtiles__/tiles/site/Data/0.b3dm', source, origin)

    expect(url).toBe('/api/v1/manager/storage-stream?engine_id=31&storage_ref=tiles%2Fsite%2FData%2F0.b3dm')
  })

  it('keeps ordinary resource URLs unchanged', () => {
    const source = { virtual: false }

    expect(buildTilesetSource('https://example.com/tileset.json', origin).rootURL).toBe('https://example.com/tileset.json')
    expect(resolveTileResourceURL('/api/v1/manager/model3d_tiles/1/assets/0.b3dm', source, origin)).toBe(
      '/api/v1/manager/model3d_tiles/1/assets/0.b3dm'
    )
  })

  it('parses storage stream URLs and encodes virtual paths', () => {
    expect(parseStorageStreamURL('/api/v1/manager/storage-stream?engine_id=1&storage_ref=a/b c/tileset.json', origin)).toEqual({
      engineID: '1',
      storageRef: 'a/b c/tileset.json'
    })
    expect(virtualTileURL('a/b c/tileset.json', origin)).toBe('http://localhost:5174/__addp_3dtiles__/a/b%20c/tileset.json')
  })
})
