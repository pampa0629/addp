import { describe, expect, it } from 'vitest'
import { parseLocatorSafe } from '@addp/common-frontend'
import {
  rasterCOGExecutionStatusTagType,
  rasterCOGExtentLabel,
  rasterCOGLastExecutionStatus,
  rasterCOGResourcePathFromLocator,
  rasterCOGResultStatusTagType,
  rasterCOGSourcePath,
  rasterCOGTaskResource,
  rasterCOGRasterSize
} from '../../src/utils/rasterCOGDisplay.js'

describe('rasterCOGDisplay', () => {
  it('formats locator paths through ResourceLocator parsing', () => {
    const locator = 'addp://engine/9/path/addp/image/srtm_40_01.tif?type=object&item_id=254'

    expect(rasterCOGResourcePathFromLocator(locator, parseLocatorSafe)).toBe('addp/image/srtm_40_01.tif')
    expect(rasterCOGTaskResource({ target: { locator } }, parseLocatorSafe)).toBe('addp/image/srtm_40_01.tif')
    expect(rasterCOGSourcePath({ locator }, parseLocatorSafe)).toBe('addp/image/srtm_40_01.tif')
  })

  it('maps task and result statuses to Element Plus tag types', () => {
    expect(rasterCOGExecutionStatusTagType('success')).toBe('success')
    expect(rasterCOGExecutionStatusTagType('running')).toBe('warning')
    expect(rasterCOGExecutionStatusTagType('failed')).toBe('danger')
    expect(rasterCOGResultStatusTagType('ready')).toBe('success')
    expect(rasterCOGResultStatusTagType('building')).toBe('warning')
    expect(rasterCOGResultStatusTagType('stale')).toBe('warning')
    expect(rasterCOGResultStatusTagType('failed')).toBe('danger')
  })

  it('normalizes raster labels and last execution status', () => {
    expect(rasterCOGRasterSize(6000, 6000)).toBe('6000 x 6000')
    expect(rasterCOGRasterSize(0, 6000)).toBe('-')
    expect(rasterCOGExtentLabel([15, 55, 20.1234567, 60])).toBe('15, 55, 20.123457, 60')
    expect(rasterCOGLastExecutionStatus({ last_execution_status: 'success' })).toBe('success')
  })
})
