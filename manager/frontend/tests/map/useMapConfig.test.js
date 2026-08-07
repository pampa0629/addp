import { beforeEach, describe, expect, it } from 'vitest'
import {
  setMapConfigAPI,
  useMapConfig
} from '../../../../common-frontend/map/src/composables/useMapConfig.js'

describe('map config profiles', () => {
  beforeEach(() => {
    setMapConfigAPI({
      getMapConfig: async () => ({
        data: {
          amap_key: 'amap-key',
          amap_security_js_code: 'security-code',
          tdt_key: 'tdt-key'
        }
      })
    })
  })

  it('keeps WGS84 basemaps first when both Tianditu and Gaode are configured', async () => {
    const { baseMapOptions, defaultBaseMapType, getBaseMapProfile, loadMapConfig } = useMapConfig()

    await loadMapConfig()

    expect(defaultBaseMapType.value).toBe('tiandituVector')
    expect(baseMapOptions.value.map((item) => item.value)).toEqual([
      'tiandituVector',
      'tiandituImage',
      'amapVector'
    ])
    expect(getBaseMapProfile('tiandituVector').coordinate_policy).toBe('wgs84')
  })

  it('marks Gaode as GCJ-02 display profile', async () => {
    const { baseMapOptions, getBaseMapProfile, loadMapConfig } = useMapConfig()

    await loadMapConfig()

    const gaodeOption = baseMapOptions.value.find((item) => item.value === 'amapVector')
    expect(gaodeOption.label).toContain('GCJ-02')
    expect(gaodeOption.profile).toEqual(getBaseMapProfile('amapVector'))
    expect(getBaseMapProfile('amapVector')).toMatchObject({
      provider: 'amap',
      view_crs: 'EPSG:3857',
      coordinate_policy: 'gcj02'
    })
  })

  it('falls back to OpenStreetMap when no keyed service is configured', async () => {
    setMapConfigAPI({
      getMapConfig: async () => ({
        data: {
          amap_key: '',
          amap_security_js_code: '',
          tdt_key: ''
        }
      })
    })
    const { baseMapOptions, defaultBaseMapType, getBaseMapProfile, loadMapConfig } = useMapConfig()

    await loadMapConfig()

    expect(defaultBaseMapType.value).toBe('osm')
    expect(baseMapOptions.value).toEqual([
      {
        label: 'OpenStreetMap',
        value: 'osm',
        profile: getBaseMapProfile('osm')
      }
    ])
    expect(getBaseMapProfile('osm')).toMatchObject({
      provider: 'osm',
      view_crs: 'EPSG:3857',
      coordinate_policy: 'wgs84'
    })
  })
})
