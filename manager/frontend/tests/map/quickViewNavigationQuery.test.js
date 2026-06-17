import { describe, expect, it } from 'vitest'
import {
  buildQuickViewOptimizationCreateQuery,
  buildTileCacheCreateQuery
} from '../../src/utils/quickViewNavigationQuery'

describe('quickViewNavigationQuery', () => {
  const target = {
    engineId: 11,
    schema: 'public',
    table: 'dltb',
    locator: 'engine://11/table/public/dltb?item_id=88',
    itemID: 88,
    geometryColumn: 'SmGeometry',
    geometryColumns: ['SmGeometry'],
    sourceSRID: 2360,
    extentSRID: 2360,
    extent: [1, 2, 3, 4]
  }

  const status = {
    item_fingerprint: 'abc123',
    quick_view: {
      geometry_column: 'SmGeometry',
      geometry_columns: ['SmGeometry', 'center'],
      source_srid: 2360,
      extent: [10, 20, 30, 40],
      extent_srid: 4326
    },
    render_facts: {
      source_srid: 2360,
      render_extent: [100, 20, 101, 21],
      render_extent_srid: 4326
    }
  }

  it('builds tile cache create query with item identity and render extent', () => {
    expect(buildTileCacheCreateQuery(target, status)).toEqual({
      tab: 'tasks',
      create: '1',
      engine_id: '11',
      schema: 'public',
      table: 'dltb',
      locator: 'engine://11/table/public/dltb?item_id=88',
      item_id: '88',
      geom: 'SmGeometry',
      item_fingerprint: 'abc123',
      geometry_columns: 'SmGeometry,center',
      source_srid: '2360',
      extent_srid: '4326',
      extent: '100,20,101,21'
    })
  })

  it('builds quick view optimization create query without tile-only extent fields', () => {
    expect(buildQuickViewOptimizationCreateQuery(target, status)).toEqual({
      tab: 'tasks',
      create: '1',
      engine_id: '11',
      schema: 'public',
      table: 'dltb',
      locator: 'engine://11/table/public/dltb?item_id=88',
      item_id: '88',
      item_fingerprint: 'abc123',
      geom: 'SmGeometry',
      geometry_columns: 'SmGeometry,center',
      source_srid: '2360'
    })
  })

  it('omits empty identity fields instead of writing blank query values', () => {
    expect(buildQuickViewOptimizationCreateQuery({
      engineId: null,
      schema: undefined,
      table: '',
      locator: null,
      itemFingerprint: undefined
    }, {})).toEqual({
      tab: 'tasks',
      create: '1'
    })
    expect(buildTileCacheCreateQuery({}, {})).toEqual({
      tab: 'tasks',
      create: '1'
    })
  })
})
