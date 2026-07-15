import { describe, expect, it } from 'vitest'
import { parseLocatorSafe } from '@addp/common-frontend'
import {
  normalizeQuickViewEngines,
  quickViewDisplayText,
  quickViewEngineName,
  quickViewResourceLabel,
  quickViewResourcePath
} from '../../src/utils/quickViewResourceDisplay.js'

describe('quickViewResourceDisplay', () => {
  it('uses the engine name instead of exposing its numeric id', () => {
    const engines = [{ id: 26, name: 'CAD 文件存储' }]

    expect(quickViewEngineName(engines, 26)).toBe('CAD 文件存储')
    expect(quickViewEngineName(engines, 99)).toBe('')
  })

  it('formats a ResourceLocator as a readable hierarchy path', () => {
    const locator = 'addp://engine/26/path/cad/Baotou_Building.dwg?type=object&item_id=81'

    expect(quickViewResourcePath(locator, parseLocatorSafe)).toBe('cad / Baotou_Building.dwg')
    expect(quickViewResourceLabel('CAD 文件存储', 'cad / Baotou_Building.dwg'))
      .toBe('CAD 文件存储 / cad / Baotou_Building.dwg')
  })

  it('replaces locators embedded in user-visible task names', () => {
    const name = '三维模型快显 - addp://engine/26/path/3d/model.glb?type=file&item_id=81'

    expect(quickViewDisplayText(name, parseLocatorSafe)).toBe('三维模型快显 - 3d / model.glb')
  })

  it('normalizes engine list API wrappers', () => {
    const engines = [{ id: 1, name: 'PostGIS' }]

    expect(normalizeQuickViewEngines({ data: engines })).toEqual(engines)
    expect(normalizeQuickViewEngines({ data: { data: engines } })).toEqual(engines)
  })
})
