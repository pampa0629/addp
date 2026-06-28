import { describe, expect, it, beforeEach } from 'vitest'
import {
  buildTilesetSource,
  parseStorageStreamURL,
  resolveTileResourceURL,
  virtualTileURL,
  withAuthToken
} from '@/utils/threeDTilesPreviewUrl'

const origin = 'http://localhost:5174'

beforeEach(() => {
  const values = new Map()
  globalThis.localStorage = {
    clear: () => values.clear(),
    getItem: (key) => values.get(key) || null,
    setItem: (key, value) => values.set(key, String(value)),
    removeItem: (key) => values.delete(key)
  }
})

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
    localStorage.setItem('token', 'token-1')
    const source = {
      rootURL: 'http://localhost:5174/__addp_3dtiles__/tiles/site/tileset.json',
      engineID: '31',
      storageRef: 'tiles/site/tileset.json',
      virtual: true
    }

    const url = resolveTileResourceURL('http://localhost:5174/__addp_3dtiles__/tiles/site/Data/0.b3dm', source, origin)

    expect(url).toBe('/api/v1/manager/storage-stream?engine_id=31&storage_ref=tiles%2Fsite%2FData%2F0.b3dm&token=token-1')
  })

  it('keeps ordinary URLs and appends auth only for ADDP API URLs', () => {
    localStorage.setItem('token', 'token-1')

    expect(withAuthToken('https://example.com/tileset.json', origin)).toBe('https://example.com/tileset.json')
    expect(withAuthToken('/api/v1/manager/storage-stream?engine_id=31', origin)).toBe('/api/v1/manager/storage-stream?engine_id=31&token=token-1')
  })

  it('parses storage stream URLs and encodes virtual paths', () => {
    expect(parseStorageStreamURL('/api/v1/manager/storage-stream?engine_id=1&storage_ref=a/b c/tileset.json', origin)).toEqual({
      engineID: '1',
      storageRef: 'a/b c/tileset.json'
    })
    expect(virtualTileURL('a/b c/tileset.json', origin)).toBe('http://localhost:5174/__addp_3dtiles__/a/b%20c/tileset.json')
  })
})
