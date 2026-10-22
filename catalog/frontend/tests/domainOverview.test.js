import { describe, it, expect, vi } from 'vitest'
import { domainOverviewState, domainOverviewQuery, referenceOwnership, loadDomainReferenceGroup } from '../src/utils/domainOverview'
import { buildEntryListQuery } from '../src/utils/entryRouteState'
import { buildEntryEditForm, buildUpdatePayload, requiredCurationGaps, withRequiredResponsibilityRows } from '../src/utils/entryEdit'

const group = () => ({ key: 'catalog', path: '/catalog/domain_standard_references', permission: 'catalog.entry.read', page: 1, version: 0 })
describe('domain overview contract', () => {
 it('retains int64 domain identities and removes unauthorized inventory scope', () => {
  expect(domainOverviewState({ domain_id: '9007199254740993', view: 'inventory' }, false)).toEqual({ domainId: '9007199254740993', view: 'governance' })
  expect(domainOverviewQuery({ domainId: '31', view: 'inventory' })).toEqual({ domain_id: '31', view: 'inventory' })
  expect(domainOverviewState({ domain_id: '01' }, true).domainId).toBe('')
 })
 it.each(['element', 'glossary', 'code_set', 'standard_document'])('preserves the new resource type %s in drill-down URLs', type => {
  expect(buildEntryListQuery({ entry_type: type, primary_domain_id: '31', view: 'inventory' })).toMatchObject({ entry_type: type, primary_domain_id: '31', view: 'inventory' })
 })
 it('distinguishes local, external, public and unknown targets without inferring ownership', () => {
  expect(referenceOwnership({ found: true, owner_domain_id: '31' }, '31')).toBe('local')
  expect(referenceOwnership({ found: true, owner_domain_id: '32' }, '31')).toBe('external')
  expect(referenceOwnership({ found: true, scope_type: 'tenant_common' }, '31')).toBe('public')
  expect(referenceOwnership({ found: true }, '31')).toBe('unavailable')
  expect(referenceOwnership({ found: false, scope_type: 'tenant_common' }, '31')).toBe('unavailable')
 })
 it('does not query owners when permission is missing', async () => {
  const state = group(), read = vi.fn()
  await loadDomainReferenceGroup(state, { domainId: '31', view: 'inventory', hasPermission: () => false, read })
  expect(state.status).toBe('forbidden'); expect(read).not.toHaveBeenCalled()
 })
 it('filters same-domain references while preserving evidence, unavailable targets, and owner pagination', async () => {
  const state = group()
  const refs = ['11', '12', '13'].map(source_identity => ({ source_type: 'element', source_identity, evidence: [{ resource_name: 'Orders', revision_id: '123' }] }))
  await loadDomainReferenceGroup(state, { domainId: '31', view: 'inventory', hasPermission: () => true,
   read: async () => ({ data: refs, total: 7 }), resolve: async () => ({ results: [
    { source_type: 'element', source_identity: '11', found: true, owner_domain_id: '31' },
    { source_type: 'element', source_identity: '12', found: true, scope_type: 'tenant_common', entry: { id: 'entry', display_name: 'Public ID' } },
    { source_type: 'element', source_identity: '13', found: false }
   ] }) })
  expect(state.items.map(row => row.ownership)).toEqual(['public', 'unavailable'])
  expect(state.items[0].evidence[0].revision_id).toBe('123')
  expect(state.total).toBe(7)
 })
 it('ignores stale results after switching domains', async () => {
  const state = group(); let release
  const stale = loadDomainReferenceGroup(state, { domainId: '31', view: 'inventory', hasPermission: () => true, read: () => new Promise(resolve => { release = resolve }), resolve: vi.fn() })
  await loadDomainReferenceGroup(state, { domainId: '32', view: 'inventory', hasPermission: () => true, read: async () => ({ data: [], total: 0 }), resolve: vi.fn() })
  release({ data: [{ source_type: 'element', source_identity: '1' }], total: 8 }); await stale
  expect(state).toMatchObject({ items: [], total: 0, status: 'ready' })
 })
 it('uses Standard definition evidence instead of editable duplicate text', () => {
  const form = withRequiredResponsibilityRows(buildEntryEditForm({ version: 1, source: { source_module: 'standard', observed_snapshot: { name: 'Standard name', scope_type: 'tenant_common', definition_complete: true } } }))
  form.governanceStatus = 'curated'; form.visibility = 'tenant'
  form.responsibilities.forEach((row, index) => { row.subjectId = String(index + 1) })
  expect(requiredCurationGaps(form)).toEqual([])
  form.businessName = 'Injected duplicate'; form.businessDescription = 'Injected definition'
  expect(buildUpdatePayload(form)).toMatchObject({ business_name: null, business_description: null })
 })
})
