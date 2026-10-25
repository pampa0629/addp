// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import EntryEditor from '../src/components/EntryEditor.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), error: vi.fn(), domainControl: null }))
vi.mock('../src/api/catalog', () => ({ listReferenceCandidates: api.list }))
vi.mock('element-plus', () => ({ ElMessage: { error: api.error } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', () => ({
  BusinessDomainSelect: { setup: (_, { attrs }) => () => {
    api.domainControl = attrs
    return h('div', { 'data-select': attrs.placeholder }, (attrs.options || []).map(option => h('span', option.name)))
  } },
  businessDomainReferenceOptions: options => options
}))

const mounted = []
const settle = async () => { for (let i = 0; i < 5; i++) { await Promise.resolve(); await nextTick() } }
function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function entry(id, version = 1) {
  return {
    id, version, governance_status: 'curated', business_name: `${id} resource`, business_description: 'A resource', visibility: 'tenant',
    semantic_links: [
      { semantic_type: 'domain', semantic_id: '10', relation_role: 'primary', observed_snapshot: { name: `${id} domain` } },
      { semantic_type: 'glossary', semantic_id: '20', relation_role: 'applies', observed_snapshot: { name: `${id} glossary` } }
    ],
    responsibilities: [
      { role: 'accountable_department', subject_type: 'department', subject_id: '30', status: 'active', observed_snapshot: { name: `${id} department` } },
      { role: 'business_owner', subject_type: 'user', subject_id: '40', status: 'active', observed_snapshot: { name: `${id} user` } },
      { role: 'data_steward', subject_type: 'user', subject_id: '41', status: 'active', observed_snapshot: { name: `${id} steward` } }
    ]
  }
}
function mount() {
  const state = reactive({ entry: entry('first') })
  const controls = new Map()
  const root = document.createElement('div')
  document.body.appendChild(root)
  const app = createApp({ render: () => h(EntryEditor, state) })
  app.component('el-select', { setup: (_, { attrs, slots }) => () => {
    controls.set(attrs.placeholder, attrs)
    return h('div', { 'data-select': attrs.placeholder }, slots.default?.())
  } })
  app.component('el-option', { props: ['label'], setup: props => () => h('span', props.label) })
  app.component('el-card', { setup: (_, { slots }) => () => h('div', [...(slots.header?.() || []), ...(slots.default?.() || [])]) })
  for (const name of ['alert', 'form', 'form-item', 'row', 'col', 'input', 'empty', 'tag', 'button']) {
    app.component(`el-${name}`, { setup: (_, { slots }) => () => h('div', slots.default?.()) })
  }
  app.mount(root)
  const record = { app, root }
  mounted.push(record)
  const key = type => `catalog.edit.${type}Placeholder`
  const control = type => type === 'domain' ? api.domainControl : controls.get(key(type))
  return {
    state,
    search: (type, query) => control(type)['remote-method'](query),
    options: type => root.querySelector(`[data-select="${key(type)}"]`)?.textContent || '',
    loading: type => control(type).loading,
    unmount: () => { app.unmount(); root.remove(); mounted.splice(mounted.indexOf(record), 1) }
  }
}
beforeEach(() => vi.resetAllMocks())
afterEach(() => mounted.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() }))

describe('entry editor candidate query lifecycle', () => {
  it.each(['domain', 'glossary', 'department', 'user'])('discards an earlier %s query when another entry is loaded', async type => {
    const pending = deferred()
    api.list.mockReturnValue(pending.promise)
    const view = mount()
    const query = view.search(type, 'old'); await settle()
    expect(view.loading(type)).toBe(true)
    view.state.entry = entry('second'); await settle()
    expect(view.loading(type)).toBe(false)
    pending.resolve({ data: [{ id: '90', name: 'Old result' }] })
    await query; await settle()
    expect(view.options(type)).not.toContain('Old result')
    expect(view.options(type)).toContain(`second ${type}`)
  })

  it('invalidates candidates when the same entry is reloaded with a new version', async () => {
    const pending = deferred()
    api.list.mockReturnValue(pending.promise)
    const view = mount()
    const query = view.search('user', 'old')
    view.state.entry.version = 2; await settle()
    pending.resolve({ data: [{ id: '90', name: 'Old result' }] })
    await query; await settle()
    expect(view.options('user')).not.toContain('Old result')
    expect(view.options('user')).toContain('first user')
    expect(view.loading('user')).toBe(false)
  })

  it.each(['reload', 'unmount'])('does not show an earlier query error after %s', async action => {
    const pending = deferred()
    api.list.mockReturnValue(pending.promise)
    const view = mount()
    const query = view.search('user', 'old')
    if (action === 'reload') view.state.entry = entry('second')
    else view.unmount()
    await settle(); pending.reject(new Error('old request failed'))
    await query; await settle()
    expect(api.error).not.toHaveBeenCalled()
  })

  it('keeps the latest query and selected names while reporting current errors', async () => {
    const old = deferred(), latest = deferred()
    api.list.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const view = mount()
    const first = view.search('user', 'old'), second = view.search('user', 'new')
    latest.resolve({ data: [{ id: '90', name: 'Latest result' }] }); await second
    old.resolve({ data: [{ id: '91', name: 'Old result' }] }); await first; await settle()
    expect(view.options('user')).toContain('Latest result')
    expect(view.options('user')).toContain('first user')
    expect(view.options('user')).not.toContain('Old result')
    expect(view.loading('user')).toBe(false)
    api.list.mockRejectedValue(new Error('current request failed'))
    await view.search('user', 'failed')
    expect(api.error).toHaveBeenCalledExactlyOnceWith('catalog.edit.candidateSearchFailed')
  })
})
