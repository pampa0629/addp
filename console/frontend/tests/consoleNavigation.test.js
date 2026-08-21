import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import { buildConsoleNavigationRequest } from '../../../common-frontend/basic/src/utils/taskOwnerUrl'
import {
  buildConsoleModuleRoute,
  navigateConsoleModuleRoute
} from '../../../common-frontend/basic/src/utils/moduleRouteNavigation'
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
      synchronized: true,
      pageDescriptor: {
        title: ' 图谱浏览 ',
        subject: ' 企业关系图谱 '
      }
    })).toEqual({
      route: '/manager/data-explorer?tab=profile',
      history: 'replace',
      synchronized: true,
      pageDescriptor: {
        title: '图谱浏览',
        subject: '企业关系图谱',
        recent: true
      }
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
    expect(portalSource).toContain('iframeNavigationKey.value += 1')
    expect(portalSource).toContain("targetModule !== currentModule.value")

    const iframeSource = readFileSync(new URL('../src/components/portal/PortalIframe.vue', import.meta.url), 'utf8')
    expect(iframeSource).toContain(':key="iframeKey || iframeUrl"')
  })

  it('places configuration management under the System navigation group', () => {
    const configSource = readFileSync(new URL('../src/config/portalConfig.js', import.meta.url), 'utf8')
    expect(configSource).not.toContain("key: 'configuration'")
    expect(configSource).not.toContain("configuration: '/configuration'")
    expect(configSource).toContain("index: '/configuration',       icon: SetUp")

    const portalSource = readFileSync(new URL('../src/views/Portal.vue', import.meta.url), 'utf8')
    expect(portalSource).not.toContain('group.isConfiguration')
    expect(portalSource).toContain("if (module === 'configuration')")
    expect(portalSource).toContain("activeGroup.value = 'system'")
    expect(portalSource).toContain("sidebarModules.value = ['system']")
  })

  it('builds a Console route from one module-local fullPath', () => {
    expect(buildConsoleModuleRoute('develop', '/workflow?action=edit&id=544'))
      .toBe('/develop/workflow?action=edit&id=544')
    expect(buildConsoleModuleRoute('develop', '/')).toBe('/develop')
    expect(() => buildConsoleModuleRoute('develop', '/develop/workflow'))
      .toThrow('must not include the Console module prefix')
  })

  it('uses local replace and one Console push for iframe module navigation', async () => {
    const listeners = new Map()
    const previousWindow = globalThis.window
    const postedMessages = []
    const parent = {
      postMessage(message) {
        postedMessages.push(message)
        queueMicrotask(() => {
          listeners.get('message')?.({
            data: {
              type: 'addp:console-bridge:response',
              channel: message.channel,
              requestId: message.requestId,
              ok: true,
              data: {}
            }
          })
        })
      }
    }
    globalThis.window = {
      parent,
      addEventListener(type, listener) {
        listeners.set(type, listener)
      },
      removeEventListener(type, listener) {
        if (listeners.get(type) === listener) listeners.delete(type)
      },
      setTimeout,
      clearTimeout
    }

    const calls = []
    const router = {
      currentRoute: { value: { fullPath: '/tasks' } },
      resolve: location => ({ fullPath: `${location.path}?action=edit&id=${location.query.id}` }),
      async push(location) {
        calls.push(['push', location])
      },
      async replace(location) {
        calls.push(['replace', location])
        this.currentRoute.value.fullPath = this.resolve(location).fullPath
      }
    }

    try {
      await navigateConsoleModuleRoute(router, 'develop', {
        path: '/workflow',
        query: { action: 'edit', id: '544' }
      })
    } finally {
      if (previousWindow === undefined) delete globalThis.window
      else globalThis.window = previousWindow
    }

    expect(calls).toEqual([['replace', {
      path: '/workflow',
      query: { action: 'edit', id: '544' }
    }]])
    expect(postedMessages).toHaveLength(1)
    expect(postedMessages[0].payload).toEqual({
      route: '/develop/workflow?action=edit&id=544',
      history: 'push',
      synchronized: true
    })
  })
})
