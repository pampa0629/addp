import { describe, expect, it, vi } from 'vitest'
import { DOMAIN_QUALITY_SECTIONS, domainQualityDisplay, domainQualityQuery, loadDomainQualitySection } from '../src/utils/domainQualitySummary'

describe('domain-owned Quality summary', () => {
  it('keeps independent Quality permissions and exact ownership filters', () => {
    expect(DOMAIN_QUALITY_SECTIONS.map(section => [section.key, section.permission])).toEqual([
      ['rules', 'quality.rule.read'],
      ['plans', 'quality.plan.read'],
      ['issues', 'quality.issue.read']
    ])
    expect(domainQualityQuery('7', 'plans')).toEqual({ owner_domain_id: '7', page: 1, page_size: 3 })
    expect(domainQualityQuery('7', 'issues')).toEqual({ owner_domain_id: '7', page: 1, page_size: 3, status: 'open' })
  })

  it('uses owner labels without treating issue targets as Catalog resource identity', () => {
    expect(domainQualityDisplay({ id: 1, name: 'Check participation', code: 'check_participation' }, 'plans')).toEqual({ id: 1, name: 'Check participation', code: 'check_participation' })
    expect(domainQualityDisplay({ id: 2, table_name: 'activities', column_name: 'member_id', type: 'not_null' }, 'issues')).toEqual({ id: 2, name: 'activities.member_id', code: 'not_null' })
  })

  it('does not call a professional owner when read permission is absent', async () => {
    const section = { ...DOMAIN_QUALITY_SECTIONS[0], requestVersion: 0, status: 'ready', total: 2, items: [{ id: 1 }] }
    const list = vi.fn()
    await loadDomainQualitySection(section, { domainId: '7', hasPermission: () => false, list })
    expect(list).not.toHaveBeenCalled()
    expect(section).toMatchObject({ status: 'forbidden', total: 0, items: [] })
  })

  it('ignores stale domain results and distinguishes 403 from an unavailable owner', async () => {
    const section = { ...DOMAIN_QUALITY_SECTIONS[2], requestVersion: 0, status: 'idle', total: 0, items: [] }
    let releaseFirst
    const first = loadDomainQualitySection(section, {
      domainId: '7', hasPermission: () => true,
      list: () => new Promise(resolve => { releaseFirst = resolve })
    })
    await loadDomainQualitySection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => ({ data: [{ id: 8, table_name: 'current' }], total: 1 })
    })
    releaseFirst({ data: [{ id: 7, table_name: 'stale' }], total: 99 })
    await first
    expect(section).toMatchObject({ status: 'ready', total: 1, items: [{ id: 8, name: 'current' }] })

    await loadDomainQualitySection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => { throw { response: { status: 403 } } }
    })
    expect(section).toMatchObject({ status: 'forbidden', total: 0, items: [] })
    await loadDomainQualitySection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => { throw new Error('Quality unavailable') }
    })
    expect(section).toMatchObject({ status: 'unavailable', total: 0, items: [] })
  })
})
