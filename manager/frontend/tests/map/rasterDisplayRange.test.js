import { describe, expect, it } from 'vitest'
import {
  isRasterNoDataSample,
  rasterDisplayRangeFromGDALMetadata,
  rasterDisplayRangeFromMeta,
  rasterDisplayRangeFromSamples,
  rasterPercentile,
  rasterSampleSize
} from '../../src/utils/rasterDisplayRange'

describe('rasterDisplayRange', () => {
  it('calculates display range with NoData filtered out', () => {
    const samples = [-32768, -10, 0, 10, 20, 30, 40, 1000]
    const range = rasterDisplayRangeFromSamples(samples, -32768)

    expect(range).toEqual({
      min: -10,
      max: 40,
      nodata: -32768
    })
  })

  it('returns null when all samples are NoData', () => {
    expect(rasterDisplayRangeFromSamples([-9999, -9999], -9999)).toBeNull()
  })

  it('calculates stable percentile and sample size', () => {
    expect(rasterPercentile([1, 2, 3, 4, 5], 0.5)).toBe(3)
    expect(isRasterNoDataSample(Number.NaN, -9999)).toBe(true)
    expect(rasterSampleSize(6000, 6000, 65536)).toEqual({ width: 256, height: 256 })
  })

  it('uses meta scan display range before runtime sampling', () => {
    expect(rasterDisplayRangeFromMeta({
      display_min: -49,
      display_max: 406,
      sample_min: -100,
      sample_max: 1000,
      nodata: -32768
    })).toEqual({
      min: -49,
      max: 406,
      nodata: -32768
    })
  })

  it('falls back to meta sample range when display range is absent', () => {
    expect(rasterDisplayRangeFromMeta({
      sample_min: 0,
      sample_max: 255
    })).toEqual({
      min: 0,
      max: 255,
      nodata: undefined
    })
  })

  it('uses GDAL metadata statistics from an opened TIFF or COG', () => {
    expect(rasterDisplayRangeFromGDALMetadata({
      STATISTICS_MINIMUM: '-49',
      STATISTICS_MAXIMUM: '406'
    }, -32768)).toEqual({
      min: -49,
      max: 406,
      nodata: -32768
    })
  })
})
