import { describe, expect, it, vi } from 'vitest'
import zhCn from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'
import { buildGovernanceStatusEntryQuery, buildMissingCoverageEntryQuery, coverageDimensionLabel, coverageGapDetailTab } from '../src/utils/governanceCoverageView'
import { parseEntryListRoute } from '../src/utils/entryRouteState'

describe('catalog governance coverage view', () => {
  it('does not construct an i18n key for an empty Element Plus table placeholder row', () => {
    const translate = vi.fn()

    expect(coverageDimensionLabel(translate, undefined, 'name')).toBe('-')
    expect(coverageDimensionLabel(translate, undefined, 'description')).toBe('-')
    expect(translate).not.toHaveBeenCalled()
  })

  it('translates a canonical coverage dimension field', () => {
    const translate = vi.fn(key => `translated:${key}`)

    expect(coverageDimensionLabel(translate, 'primary_domain', 'name')).toBe(
      'translated:catalog.coverage.dimensions.primary_domain.name'
    )
  })

  it('builds the canonical missing-coverage inventory query only for fixed dimensions', () => {
    expect(buildMissingCoverageEntryQuery('accountable_department')).toEqual({
      view: 'inventory', coverage_dimension: 'accountable_department', coverage_state: 'missing'
    })
    expect(buildMissingCoverageEntryQuery('component_standard_mapping')).toEqual({
      view: 'inventory', coverage_dimension: 'component_standard_mapping', coverage_state: 'missing'
    })
    expect(buildMissingCoverageEntryQuery('component_element')).toBeNull()
    expect(buildMissingCoverageEntryQuery('accountability')).toBeNull()
    expect(buildMissingCoverageEntryQuery('unknown')).toBeNull()
  })

  it('opens item-level governance gaps in curation while leaving batch-assignment gaps on the default detail tab', () => {
    for (const dimension of ['business_definition', 'business_owner', 'data_steward', 'glossary', 'component_standard_mapping']) {
      expect(coverageGapDetailTab(dimension, 'missing')).toBe('curation')
    }
    for (const dimension of ['primary_domain', 'accountable_department', 'unknown']) {
      expect(coverageGapDetailTab(dimension, 'missing')).toBe('')
    }
    expect(coverageGapDetailTab('business_definition', '')).toBe('')
  })

  it('drills governance status counts into the inventory without treating them as coverage gaps', () => {
    for (const status of ['discovered', 'curated', 'certified', 'deprecated']) {
      const query = buildGovernanceStatusEntryQuery(status)
      expect(query).toEqual({ view: 'inventory', governance_status: status })
      expect(parseEntryListRoute(query)).toMatchObject({ view: 'inventory', governance_status: status, coverage_dimension: '' })
    }
    expect(buildGovernanceStatusEntryQuery('unknown')).toBeNull()
  })

  it('provides bilingual handling guidance for every coverage gap and keeps curation separate from assignment', () => {
    const dimensions = [
      'business_definition', 'primary_domain', 'accountable_department', 'business_owner',
      'data_steward', 'glossary', 'component_standard_mapping'
    ]
    expect(Object.keys(zhCn.catalog.entries.coverageGapGuidance).sort()).toEqual([...dimensions].sort())
    expect(Object.keys(en.catalog.entries.coverageGapGuidance).sort()).toEqual([...dimensions].sort())
    expect(zhCn.catalog.entries.discoveredGuidance).toContain('不会改变编目状态')
    expect(zhCn.catalog.entries.coverageGapGuidance.primary_domain).toContain('批量分配')
    expect(zhCn.catalog.entries.coverageGapGuidance.accountable_department).toContain('批量分配')
    expect(zhCn.catalog.entries.coverageGapGuidance.business_definition).toContain('还需满足主业务域和责任关系')
    expect(en.catalog.entries.coverageGapGuidance.business_definition).toContain('also requires a primary domain')
    expect(en.catalog.entries.discoveredGuidance).toContain('does not change its curation status')
  })
})
