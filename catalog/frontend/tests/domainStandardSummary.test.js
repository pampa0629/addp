import { describe, expect, it, vi } from 'vitest'
import { DOMAIN_STANDARD_SECTIONS, domainStandardDisplay, domainStandardQuery, loadDomainStandardSection } from '../src/utils/domainStandardSummary'

describe('domain-owned Standard summary', () => {
  it('keeps three professional owners and independent read permissions', () => {
    expect(DOMAIN_STANDARD_SECTIONS.map(section => [section.key, section.permission])).toEqual([
      ['glossaries', 'standard.glossary.read'],
      ['elements', 'standard.element.read'],
      ['metrics', 'standard.metric.read']
    ])
  })

  it('queries only the selected exact domain, never its parent or common scopes', () => {
    expect(domainStandardQuery('7', 1)).toEqual({ scope_type: 'domain', owner_domain_id: '7', page: 1, page_size: 5 })
  })

  it('prefers the current professional revision and identifies draft-only rows honestly', () => {
    expect(domainStandardDisplay({ id: 2, code: 'active_code', current_revision: { name: 'Current', status: 'published' }, draft_revision: { name: 'Next', status: 'draft' } })).toEqual({
      id: 2, name: 'Current', code: 'active_code', revisionStatus: 'published'
    })
    expect(domainStandardDisplay({ id: 3, code: 'draft_code', draft_revision: { name: 'New', status: 'draft' } })).toEqual({
      id: 3, name: 'New', code: 'draft_code', revisionStatus: 'draft'
    })
  })

  it('does not call Standard when the current user lacks that type-specific permission', async () => {
    const section = { ...DOMAIN_STANDARD_SECTIONS[0], page: 1, requestVersion: 0, items: [{ id: 1 }], total: 1, status: 'ready' }
    const list = vi.fn()
    await loadDomainStandardSection(section, { domainId: '7', hasPermission: () => false, list })
    expect(list).not.toHaveBeenCalled()
    expect(section).toMatchObject({ status: 'forbidden', items: [], total: 0 })
  })

  it('keeps authorization denial separate from owner unavailability and ignores stale domain results', async () => {
    const section = { ...DOMAIN_STANDARD_SECTIONS[1], page: 1, requestVersion: 0, items: [], total: 0, status: 'idle' }
    let releaseFirst
    const first = loadDomainStandardSection(section, {
      domainId: '7', hasPermission: () => true,
      list: () => new Promise(resolve => { releaseFirst = resolve })
    })
    await loadDomainStandardSection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => ({ data: [{ id: 8, code: 'current' }], total: 1 })
    })
    releaseFirst({ data: [{ id: 7, code: 'stale' }], total: 99 })
    await first
    expect(section).toMatchObject({ status: 'ready', total: 1, items: [{ id: 8, name: 'current' }] })

    await loadDomainStandardSection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => { throw { response: { status: 403 } } }
    })
    expect(section).toMatchObject({ status: 'forbidden', items: [], total: 0 })
    await loadDomainStandardSection(section, {
      domainId: '8', hasPermission: () => true,
      list: async () => { throw new Error('Standard unavailable') }
    })
    expect(section).toMatchObject({ status: 'unavailable', items: [], total: 0 })
  })
})
