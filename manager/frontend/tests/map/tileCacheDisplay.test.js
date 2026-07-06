import { describe, expect, it } from 'vitest'
import { resourceTextFromLocator, taskResource } from '../../src/utils/tileCacheDisplay'

const parseLocator = (locator) => {
  const match = String(locator || '').match(/^addp:\/\/engine\/(\d+)\/path\/([^?]*)(?:\?(.*))?$/)
  const params = new URLSearchParams(match?.[3] || '')
  return {
    engineId: Number(match?.[1] || 0),
    path: decodeURIComponent(String(match?.[2] || '')).split('/').filter(Boolean),
    type: params.get('type') || ''
  }
}

describe('tileCacheDisplay', () => {
  it('uses table notation for table locators', () => {
    expect(resourceTextFromLocator(
      'addp://engine/7/path/public/roads?type=table&item_id=21',
      parseLocator
    )).toBe('public.roads')
  })

  it('uses path notation for file and object locators', () => {
    expect(resourceTextFromLocator(
      'addp://engine/9/path/addp/gis/%E8%A7%84%E5%88%92%E7%94%A8%E5%9C%B0.shp?type=object&item_id=236',
      parseLocator
    )).toBe('addp/gis/规划用地.shp')
  })

  it('falls back to locator path for file task resources without schema and table', () => {
    expect(taskResource({
      config: {
        target: {
          locator: 'addp://engine/9/path/addp/gis/a2.shp?type=object&item_id=284'
        }
      }
    }, parseLocator)).toBe('addp/gis/a2.shp')
  })
})
