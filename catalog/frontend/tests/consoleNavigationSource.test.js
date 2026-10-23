import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { allowsConsoleRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

const read = path => readFileSync(new URL(`../../../${path}`, import.meta.url), 'utf8')

describe('Catalog cross-module navigation bridge', () => {
  it('uses complete Console entry access for list and detail source links', () => {
    const list = read('catalog/frontend/src/views/EntryList.vue')
    expect(list).toContain(':disabled="!canOpenStandardDefinition(row)"')
    expect(list).toContain('if (canOpenStandardDefinition(row)) openConsoleRoute(config.detailPath)')
    const detail = read('catalog/frontend/src/views/EntryDetail.vue')
    expect(detail).toContain('allowsConsoleRoute(path, authStore.contextType, authStore.permissions)')
    expect(detail).toContain("? resolveConsoleRouteUrl(path) : ''")
  })

  it.each([
    ['glossaries', 'standard.glossary.read'], ['elements', 'standard.element.read'],
    ['code-sets', 'standard.code_set.read'], ['documents', 'standard.document.read'],
    ['metrics', 'standard.metric.read']
  ])('keeps %s source entry closed without page dependencies', (resource, permission) => {
    const path = `/standard/${resource}/3?revision_id=7`
    const permissions = [permission]
    expect(allowsConsoleRoute(path, 'tenant', permissions)).toBe(false)
    permissions.push('standard.domain.read')
    if (resource === 'metrics') {
      expect(allowsConsoleRoute(path, 'tenant', permissions)).toBe(false)
      permissions.push('standard.unit.read')
    }
    expect(allowsConsoleRoute(path, 'tenant', permissions)).toBe(true)
    expect(allowsConsoleRoute(path, 'platform', permissions)).toBe(false)
    permissions.splice(permissions.indexOf('standard.domain.read'), 1)
    expect(allowsConsoleRoute(path, 'tenant', permissions)).toBe(false)
    expect(allowsConsoleRoute('/unregistered/3', 'tenant', permissions)).toBe(false)
  })

  it('keeps professional content exclusively in the independent overview', () => {
    const source = read('catalog/frontend/src/views/EntryList.vue')
    expect(source).toContain('catalog-domain-overview-link')
    expect(source).not.toContain('DomainProfessionalSummary')
    expect(source).not.toContain('professionalOpen')
    const overview = read('catalog/frontend/src/views/DomainOverview.vue')
    expect(overview).toContain('<DomainQualitySummary')
    expect(overview).toContain('catalog-domain-reference-summary')
  })

  it('uses the shared Console navigation source in every Catalog owner link', () => {
    const console = read('console/frontend/src/views/Portal.vue')
    const sharedNavigation = read('common-frontend/basic/src/utils/taskOwnerUrl.js')
    expect(console).toMatch(/allowedSources:\s*\['addp-module'\]/)
    expect(sharedNavigation).toMatch(/source = 'addp-module'/)

    for (const path of [
      'catalog/frontend/src/components/DomainQualitySummary.vue',
      'catalog/frontend/src/views/EntryDetail.vue'
    ]) {
      const source = read(path)
      expect(source).toContain('openConsoleRoute(')
      expect(source).not.toMatch(/\bsource:\s*['"]addp-catalog['"]/) // Console ignores unregistered sources.
    }
  })
})
