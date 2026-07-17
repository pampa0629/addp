import { describe, expect, it } from 'vitest'

import { buildModuleUrl } from '../../../console/frontend/src/config/portalConfig'

describe('Console module URL', () => {
  it('never includes an access token in an iframe URL', () => {
    const url = new URL(buildModuleUrl('manager', 'data-explorer?view=map', 'secret-token'))

    expect(url.pathname).toContain('/data-explorer')
    expect(url.searchParams.get('view')).toBe('map')
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.href).not.toContain('secret-token')
  })
})
