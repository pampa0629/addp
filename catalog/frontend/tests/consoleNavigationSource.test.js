import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const read = path => readFileSync(new URL(`../../../${path}`, import.meta.url), 'utf8')

describe('Catalog cross-module navigation bridge', () => {
  it('loads domain-owned professional sections only inside the expanded domain card', () => {
    const source = read('catalog/frontend/src/views/EntryList.vue')
    const cardStart = source.indexOf('data-testid="catalog-domain-context"')
    const toggle = source.indexOf('data-testid="catalog-domain-professional-toggle"', cardStart)
    const summary = source.indexOf('<DomainProfessionalSummary', toggle)
    const cardEnd = source.indexOf('</el-card>', summary)
    expect(cardStart).toBeGreaterThan(0)
    expect(toggle).toBeGreaterThan(cardStart)
    expect(summary).toBeGreaterThan(toggle)
    expect(cardEnd).toBeGreaterThan(summary)
    expect(source).toContain('v-if="selectedDomain && professionalOpen"')
    expect(source).not.toContain('<DomainStandardSummary')
  })

  it('uses the shared Console navigation source in every Catalog owner link', () => {
    const console = read('console/frontend/src/views/Portal.vue')
    const sharedNavigation = read('common-frontend/basic/src/utils/taskOwnerUrl.js')
    expect(console).toMatch(/allowedSources:\s*\['addp-module'\]/)
    expect(sharedNavigation).toMatch(/source = 'addp-module'/)

    for (const path of [
      'catalog/frontend/src/components/DomainProfessionalSummary.vue',
      'catalog/frontend/src/views/EntryDetail.vue'
    ]) {
      const source = read(path)
      expect(source).toContain('openConsoleRoute(')
      expect(source).not.toMatch(/\bsource:\s*['"]addp-catalog['"]/) // Console ignores unregistered sources.
    }
  })
})
