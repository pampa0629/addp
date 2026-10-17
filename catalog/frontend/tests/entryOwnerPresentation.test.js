import { describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import en from '../src/i18n/en.json'
import zhCn from '../src/i18n/zh-cn.json'
import { isProfessionalOwner, loadOwnerMetricReferences, professionalOwnerName, resolveOwnerPrimaryDomain } from '../src/utils/entryOwnerPresentation.js'

describe('Catalog entry professional owner presentation', () => {
  it('recognizes every professional source owner including Workbench', () => {
    for (const [sourceModule, ownerName] of Object.entries({
      model: 'Model', standard: 'Standard', service: 'Service', develop: 'Develop', workbench: 'Workbench'
    })) {
      expect(isProfessionalOwner(sourceModule)).toBe(true)
      expect(professionalOwnerName(sourceModule)).toBe(ownerName)
    }
  })

  it('rejects sources that do not expose a professional resolver', () => {
    expect(isProfessionalOwner('meta')).toBe(false)
    expect(professionalOwnerName('meta')).toBe('')
  })

  it('opens owner details at the top-level application origin', () => {
    const source = readFileSync(new URL('../src/views/EntryDetail.vue', import.meta.url), 'utf8')
    expect(source).toContain(':href="ownerDetailUrl"')
    expect(source).toContain('target="_top"')
    expect(source).not.toContain('@click="openOwnerDetail"')
  })

  it('resolves the actual owner domain name from the Standard hierarchy', () => {
    const domains = [{ id: '1', name: '业务域', children: [{ id: '2', name: '户外域' }] }]
    expect(resolveOwnerPrimaryDomain(domains, 2)?.name).toBe('户外域')
    expect(resolveOwnerPrimaryDomain(domains, 3)).toBeNull()
    expect(resolveOwnerPrimaryDomain(null, 2)).toBeNull()
  })

  it('keeps owner domain labels under the detail namespace in both languages', () => {
    for (const messages of [zhCn, en]) {
      expect(messages.catalog.entry.ownerManagedDomain).toBeTruthy()
      expect(messages.catalog.entry.publicScope).toBeTruthy()
      expect(messages.catalog.entries.ownerManagedDomain).toBeUndefined()
      expect(messages.catalog.entries.publicScope).toBeUndefined()
    }
  })
})

describe('owner metric display references', () => {
  const metric = { entry_type: 'metric', source: { source_module: 'standard' }, source_resolution: { summary: { category_id: '41', unit_id: '51' } } }
  const state = () => ({ category: { name: 'Previous category' }, unit: { name: 'Previous unit' }, requestVersion: 0 })
  const requests = () => ({ hasPermission: () => true,
    listCategories: vi.fn(async () => [{ id: 41, name: '活动统计' }]), getUnit: vi.fn(async () => ({ id: 51, name: '次' })) })

  it('resolves exact names from the current owner summary rather than the observed snapshot', async () => {
    const refs = state(), api = requests()
    await loadOwnerMetricReferences(refs, { ...api, entry: { ...metric, source: { ...metric.source, observed_snapshot: { category_id: '9', unit_id: '8' } } } })
    expect(refs.category?.name).toBe('活动统计')
    expect(refs.unit?.name).toBe('次')
    expect(api.getUnit).toHaveBeenCalledExactlyOnceWith('51')
  })

  it.each(['standard.metric.read', 'standard.unit.read'])('keeps %s independent from the other owner permission', async denied => {
    const refs = state(), api = requests()
    await loadOwnerMetricReferences(refs, { ...api, entry: metric, hasPermission: permission => permission !== denied })
    expect(api.listCategories).toHaveBeenCalledTimes(denied === 'standard.metric.read' ? 0 : 1)
    expect(api.getUnit).toHaveBeenCalledTimes(denied === 'standard.unit.read' ? 0 : 1)
    expect(denied === 'standard.metric.read' ? refs.category : refs.unit).toBeNull()
  })

  it('keeps a successful unit observation when category reading fails and clears prior names', async () => {
    const refs = state(), api = requests()
    api.listCategories.mockRejectedValue({ response: { status: 403 } })
    await loadOwnerMetricReferences(refs, { ...api, entry: metric })
    expect(refs.category).toBeNull()
    expect(refs.unit?.name).toBe('次')
  })

  it('rejects a different returned unit and malformed category lists without inventing names', async () => {
    const refs = state(), api = requests()
    api.listCategories.mockResolvedValue({ data: [{ id: 41, name: 'Wrong contract' }] })
    api.getUnit.mockResolvedValue({ id: 52, name: 'Wrong unit' })
    await loadOwnerMetricReferences(refs, { ...api, entry: metric })
    expect(refs).toMatchObject({ category: null, unit: null })
  })

  it('does not make numeric conversions for large string identities', async () => {
    const refs = state(), api = requests(), id = '9007199254740993'
    api.listCategories.mockResolvedValue([{ id: '9007199254740992', name: 'Neighbor' }, { id, name: 'Exact category' }])
    api.getUnit.mockResolvedValue({ id, name: 'Exact unit' })
    await loadOwnerMetricReferences(refs, { ...api, entry: { ...metric, source_resolution: { summary: { category_id: id, unit_id: id } } } })
    expect(refs.category?.name).toBe('Exact category')
    expect(refs.unit?.name).toBe('Exact unit')
    expect(api.getUnit).toHaveBeenCalledExactlyOnceWith(id)
  })

  it('discards late results after switching entries or losing permissions', async () => {
    for (const next of [null, metric]) {
      const refs = state(), api = requests()
      let release
      api.getUnit.mockImplementation(() => new Promise(resolve => { release = resolve }))
      const pending = loadOwnerMetricReferences(refs, { ...api, entry: metric })
      await loadOwnerMetricReferences(refs, { ...api, entry: next, hasPermission: () => false })
      release({ id: 51, name: 'Late unit' })
      await pending
      expect(refs).toMatchObject({ category: null, unit: null })
    }
  })
})
