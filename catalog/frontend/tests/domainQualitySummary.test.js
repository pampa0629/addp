import { describe, expect, it, vi } from 'vitest'
import { DOMAIN_QUALITY_SECTIONS, domainQualityQuery, loadDomainQualitySection } from '../src/utils/domainQualitySummary'

describe('domain-owned Quality summary', () => {
  it('keeps independent Quality permissions and exact ownership filters', () => {
    expect(DOMAIN_QUALITY_SECTIONS.map(section => [section.key, section.permission])).toEqual([
      ['rules', 'quality.rule.read'],
      ['plans', 'quality.plan.read'],
      ['issues', 'quality.issue.read']
    ])
    expect(domainQualityQuery('7', 'plans')).toEqual({ owner_domain_id: '7', page: 1, page_size: 1 })
    expect(domainQualityQuery('7', 'issues')).toEqual({ owner_domain_id: '7', page: 1, page_size: 1, status: 'open' })
  })

  it('reads the owner total without consuming or storing professional objects', async () => {
    const section = { ...DOMAIN_QUALITY_SECTIONS[0], requestVersion: 0, status: 'idle', total: 0 }
    await loadDomainQualitySection(section, { domainId: '7', hasPermission: () => true,
      list: async () => ({ total: 14, get data() { throw new Error('object details must not be consumed') } }) })
    expect(section).toMatchObject({ status: 'ready', total: 14 })
    expect(section).not.toHaveProperty('items')
    await loadDomainQualitySection(section, { domainId: '7', hasPermission: () => true, list: async () => ({ data: [], total: 0 }) })
    expect(section).toMatchObject({ status: 'ready', total: 0 })
  })

  it('does not call a professional owner when read permission is absent', async () => {
    const section = { ...DOMAIN_QUALITY_SECTIONS[0], requestVersion: 0, status: 'ready', total: 2 }
    const list = vi.fn()
    await loadDomainQualitySection(section, { domainId: '7', hasPermission: () => false, list })
    expect(list).not.toHaveBeenCalled()
    expect(section).toMatchObject({ status: 'forbidden', total: 0 })
  })

  it.each([undefined, null, -1, 1.5, '3'])('does not present an invalid owner total %s as a valid count', async total => {
    const section = { ...DOMAIN_QUALITY_SECTIONS[0], requestVersion: 0, status: 'ready', total: 14 }
    await loadDomainQualitySection(section, { domainId: '7', hasPermission: () => true, list: async () => ({ total }) })
    expect(section).toMatchObject({ status: 'unavailable', total: 0 })
  })

  it('ignores stale domain results and distinguishes 403 from an unavailable owner', async () => {
    const section = { ...DOMAIN_QUALITY_SECTIONS[2], requestVersion: 0, status: 'idle', total: 0 }
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
    expect(section).toMatchObject({ status: 'ready', total: 1 })
    expect(section).not.toHaveProperty('items')

    await loadDomainQualitySection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => { throw { response: { status: 403 } } }
    })
    expect(section).toMatchObject({ status: 'forbidden', total: 0 })
    await loadDomainQualitySection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => { throw new Error('Quality unavailable') }
    })
    expect(section).toMatchObject({ status: 'unavailable', total: 0 })
  })
})
