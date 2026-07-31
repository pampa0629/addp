import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import { buildConsoleNavigationRequest } from '../../../common-frontend/basic/src/utils/taskOwnerUrl'
import { isSynchronizedIframeRoute, splitConsoleRoute } from '../src/utils/consoleNavigation'

describe('Console navigation bridge', () => {
  it('builds explicit push and synchronized replace requests', () => {
    expect(buildConsoleNavigationRequest('/monitor/executions')).toEqual({
      route: '/monitor/executions',
      history: 'push',
      synchronized: false
    })
    expect(buildConsoleNavigationRequest('/manager/data-explorer?tab=profile', {
      history: 'replace',
      synchronized: true
    })).toEqual({
      route: '/manager/data-explorer?tab=profile',
      history: 'replace',
      synchronized: true
    })
  })

  it('rejects invalid routes and history modes', () => {
    expect(() => buildConsoleNavigationRequest('//example.com')).toThrow()
    expect(() => buildConsoleNavigationRequest('/manager/data-explorer', { history: 'legacy' })).toThrow()
  })

  it('preserves the ResourceLocator query delimiter when splitting a Console route', () => {
    expect(splitConsoleRoute(
      '/manager/data-explorer?locator=addp://engine/2/path/public/farmland?type=table%26item_id=51572'
    )).toEqual([
      '/manager/data-explorer',
      'locator=addp://engine/2/path/public/farmland?type=table%26item_id=51572'
    ])
  })

  it('keeps the current iframe when the active module synchronizes its route', () => {
    expect(isSynchronizedIframeRoute(
      'manager',
      '/manager/data-explorer?locator=addp://engine/2/path/public/farmland?type=table%26item_id=51572'
    )).toBe(true)
    expect(isSynchronizedIframeRoute('manager', '/monitor/executions')).toBe(false)

    const portalSource = readFileSync(new URL('../src/views/Portal.vue', import.meta.url), 'utf8')
    expect(portalSource).toContain('isSynchronizedIframeRoute(synchronizedIframeModule, fullPath)')
    expect(portalSource).toContain('if (url && !keepCurrentIframe)')
    expect(portalSource).toContain("targetModule !== currentModule.value")
  })
})
