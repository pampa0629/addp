import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const read = path => readFileSync(new URL(`../../../${path}`, import.meta.url), 'utf8')

describe('Catalog cross-module navigation bridge', () => {
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
