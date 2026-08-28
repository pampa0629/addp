import { describe, expect, it } from 'vitest'

import {
  DASHBOARD_SCOPE_ALL,
  DASHBOARD_SCOPE_APPLICATION,
  dashboardStatsParams
} from '@/utils/dashboardScope'

describe('Asset dashboard scope', () => {
  it('maps the visible scope to the single filtered dashboard API', () => {
    expect(dashboardStatsParams(DASHBOARD_SCOPE_ALL)).toEqual({})
    expect(dashboardStatsParams(DASHBOARD_SCOPE_APPLICATION)).toEqual({ type_code: 'application' })
    expect(dashboardStatsParams('asset:42')).toEqual({ type_code: 'application', asset_id: 42 })
  })
})
