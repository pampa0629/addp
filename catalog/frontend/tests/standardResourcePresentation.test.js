import { describe, it, expect, vi } from 'vitest'
import { standardResourcePresentation, standardDefinitionRevision, loadStandardDefinition } from '../src/utils/standardResourcePresentation'
const source = { source_module: 'standard', source_type: 'element', source_identity: '9007199254740993', source_status: 'active' }
const state = () => ({ version: 0, text: '', revision: null, config: null })
describe('Standard definition ownership', () => {
 it.each(['glossary', 'element', 'code_set', 'standard_document', 'metric'])('resolves %s to its native permission and route', kind => {
  const config = standardResourcePresentation(kind, source.source_identity, '9007199254740995')
  expect(config.permission).toMatch(/^standard\./)
  expect(config.detailPath).toContain('/9007199254740993?revision_id=9007199254740995')
 })
 it('rejects noncanonical and unsupported identities', () => {
  expect(standardResourcePresentation('element', '01')).toBeNull()
  expect(standardResourcePresentation('data_item', '1')).toBeNull()
 })
 it('uses the effective revision before a newer draft', () => {
  const aggregate = { current_revision: { definition: 'Effective', status: 'published' }, latest_revision: { definition: 'Draft', status: 'draft' } }
  expect(standardDefinitionRevision(aggregate)).toEqual({ revision: aggregate.current_revision, isCurrent: true })
  expect(standardDefinitionRevision({ latest_revision: aggregate.latest_revision }).isCurrent).toBe(false)
 })
 it('reads definitions only with the owner permission and no Catalog fallback', async () => {
  const value = state(), read = vi.fn()
  await loadStandardDefinition(value, { source, hasPermission: () => false, read })
  expect(value.status).toBe('forbidden'); expect(read).not.toHaveBeenCalled(); expect(value.text).toBe('')
  await loadStandardDefinition(value, { source, hasPermission: () => true, read: async () => ({ latest_revision: { definition: 'Owner text' } }) })
  expect(value.text).toBe('Owner text'); expect(value.revision.isCurrent).toBe(false)
 })
 it('clears old owner text on missing source or a failed request', async () => {
  const value = { ...state(), text: 'Old text' }, read = vi.fn()
  await loadStandardDefinition(value, { source: { ...source, source_status: 'missing' }, hasPermission: () => true, read })
  expect(value.status).toBe('missing'); expect(value.text).toBe(''); expect(read).not.toHaveBeenCalled()
  await loadStandardDefinition(value, { source, hasPermission: () => true, read: async () => { throw { response: { status: 403 } } } })
  expect(value.status).toBe('forbidden'); expect(value.text).toBe('')
 })
 it('discards an old definition response after switching source', async () => {
  const value = state(); let release
  const old = loadStandardDefinition(value, { source, hasPermission: () => true, read: () => new Promise(resolve => { release = resolve }) })
  await loadStandardDefinition(value, { source: { ...source, source_identity: '2' }, hasPermission: () => true, read: async () => ({ latest_revision: { definition: 'New' } }) })
  release({ current_revision: { definition: 'Old' } }); await old
  expect(value.text).toBe('New'); expect(value.config.path).toBe('/standard/elements/2')
 })
})
