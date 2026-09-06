import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import { buildConsoleNavigationRequest } from '../../../common-frontend/basic/src/utils/taskOwnerUrl'
import {
  buildConsoleModuleRoute,
  navigateConsoleModuleRoute
} from '../../../common-frontend/basic/src/utils/moduleRouteNavigation'
import { isSynchronizedIframeRoute, splitConsoleRoute } from '../src/utils/consoleNavigation'
import { searchIndex } from '../src/config/searchIndex'

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

  it('delegates fullscreen permission to the active module iframe', () => {
    const iframeSource = readFileSync(new URL('../src/components/portal/PortalIframe.vue', import.meta.url), 'utf8')
    expect(iframeSource).toContain('allow="clipboard-write; clipboard-read; fullscreen"')
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

  it('keeps the enterprise Catalog reachable from every Console discovery surface', () => {
    const configSource = readFileSync(new URL('../src/config/portalConfig.js', import.meta.url), 'utf8')
    expect(configSource).toContain("modules: ['catalog', 'asset']")
    expect(configSource).toContain("catalog:      '/catalog/entries'")
    expect(configSource).toContain("index: '/catalog/entries'")
    expect(configSource).toContain("index: '/catalog/governance/coverage'")

    const searchSource = readFileSync(new URL('../src/config/searchIndex.js', import.meta.url), 'utf8')
    expect(searchSource).toContain("module: 'catalog', route: '/catalog/entries'")
    expect(searchSource).toContain("module: 'catalog', route: '/catalog/governance/coverage'")

    const apiDocsSource = readFileSync(new URL('../src/views/ApiDocs.vue', import.meta.url), 'utf8')
    expect(apiDocsSource).toContain("name: 'catalog'")
    expect(apiDocsSource).toContain("viewer('/swagger-spec/catalog')")

    const viteSource = readFileSync(new URL('../vite.config.js', import.meta.url), 'utf8')
    expect(viteSource).toContain("'/module-health/catalog'")
    expect(viteSource).toContain("'/swagger-spec/catalog'")

    expect(searchIndex('治理覆盖率', key => key, [])).toEqual([])
    expect(searchIndex('治理覆盖率', key => key, ['catalog.inventory.read']).map(item => item.route))
      .toContain('/catalog/governance/coverage')
  })

  it('exposes Standard collections in navigation and search', () => {
    const configSource = readFileSync(new URL('../src/config/portalConfig.js', import.meta.url), 'utf8')
    const zhCn = JSON.parse(readFileSync(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
    const en = JSON.parse(readFileSync(new URL('../src/i18n/en.json', import.meta.url), 'utf8'))

    expect(configSource).toContain("index: '/standard/collections'")
    expect(searchIndex('标准集', key => key === 'console.menus.standard.collections' ? zhCn.console.menus.standard.collections : key, ['standard.collection.read']).map(item => item.route))
      .toContain('/standard/collections')
    expect(zhCn.console.menus.standard.collections).toBe('标准集管理')
    expect(en.console.menus.standard.collections).toBe('Standard Collections')
  })

  it('keeps Workbench reachable as the general data-service consumer', () => {
    const configSource = readFileSync(new URL('../src/config/portalConfig.js', import.meta.url), 'utf8')
    expect(configSource).toContain("modules: ['develop', 'service', 'workbench', 'orchestrator', 'monitor']")
    expect(configSource).toContain("workbench:    '/workbench/applications'")
    expect(configSource).toContain("index: '/workbench/applications'")
    expect(configSource).toContain("index: '/workbench/applications'")

    const searchSource = readFileSync(new URL('../src/config/searchIndex.js', import.meta.url), 'utf8')
    expect(searchSource).toContain("module: 'workbench', route: '/workbench/applications'")
    expect(searchSource).toContain("module: 'workbench', route: '/workbench/applications'")

    const apiDocsSource = readFileSync(new URL('../src/views/ApiDocs.vue', import.meta.url), 'utf8')
    expect(apiDocsSource).toContain("name: 'workbench'")
    expect(apiDocsSource).toContain("viewer('/swagger-spec/workbench')")

    const viteSource = readFileSync(new URL('../vite.config.js', import.meta.url), 'utf8')
    expect(viteSource).toContain("'/module-health/workbench'")
    expect(viteSource).toContain("'/swagger-spec/workbench'")
    expect(viteSource).toContain("'/data-apps'")
  })

  it('exposes the consolidated Security information architecture', () => {
    const configSource = readFileSync(new URL('../src/config/portalConfig.js', import.meta.url), 'utf8')
    const searchSource = readFileSync(new URL('../src/config/searchIndex.js', import.meta.url), 'utf8')
    const zhCn = JSON.parse(readFileSync(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
    const en = JSON.parse(readFileSync(new URL('../src/i18n/en.json', import.meta.url), 'utf8'))

    expect(configSource).toContain("security:     '/security/sensitive-data-definitions'")
    expect(configSource).toContain("index: '/security/classification-grading'")
    expect(configSource).toContain("index: '/security/sensitive-data-definitions'")
    expect(configSource).toContain("index: '/security/protection-baselines'")
    expect(configSource).toContain("index: '/security/protection-enrollments'")
    expect(configSource).not.toContain("index: '/security/sensitive-data-types'")
    expect(configSource).not.toContain("index: '/security/classifications'")
    expect(configSource).not.toContain("index: '/security/grades'")
    expect(searchSource).toContain("route: '/security/classification-grading'")
    expect(searchSource).toContain("route: '/security/sensitive-data-definitions'")
    expect(searchSource).toContain("route: '/security/protection-enrollments'")
    expect(zhCn.console.menus.security.classificationGrading).toBe('分类分级体系')
    expect(zhCn.console.menus.security.sensitiveDataDefinitions).toBe('敏感数据定义')
    expect(zhCn.console.menus.security.defaultProtectionRules).toBe('默认保护规则')
    expect(zhCn.console.menus.security.protectedResources).toBe('受保护资源')
    expect(en.console.menus.security.classificationGrading).toBe('Classification & Grading')
    expect(en.console.menus.security.sensitiveDataDefinitions).toBe('Sensitive Data Definitions')
    expect(en.console.menus.security.defaultProtectionRules).toBe('Default Protection Rules')
    expect(en.console.menus.security.protectedResources).toBe('Protected Resources')
  })

  it('localizes the Workbench module name for every Console discovery surface', () => {
    const zhCn = JSON.parse(readFileSync(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
    const en = JSON.parse(readFileSync(new URL('../src/i18n/en.json', import.meta.url), 'utf8'))

    expect(zhCn.console.modules.workbench.label).toBe('工作台')
    expect(zhCn.console.menus.workbench.label).toBe('工作台')
    expect(zhCn.console.menus.workbench.dataApplications).toBe('数据应用')
    expect(en.console.modules.workbench.label).toBe('Workbench')
    expect(en.console.menus.workbench.label).toBe('Workbench')
    expect(en.console.menus.workbench.dataApplications).toBe('Data Applications')
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
