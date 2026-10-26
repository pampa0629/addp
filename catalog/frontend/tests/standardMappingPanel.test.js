// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import StandardMappingPanel from '../src/components/StandardMappingPanel.vue'

const api = vi.hoisted(() => Object.fromEntries([
  'createStandardMapping', 'deleteStandardMapping', 'listReferenceCandidates',
  'listStandardMappingRevisionOptions', 'reviewStandardMapping', 'updateStandardMapping', 'error'
].map(name => [name, vi.fn()])))
vi.mock('../src/api/catalog', () => api)
vi.mock('element-plus', () => ({ ElMessage: { error: api.error }, ElMessageBox: {} }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))

const mounted = []
const settle = async () => { for (let i = 0; i < 5; i++) { await Promise.resolve(); await nextTick() } }
function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const mapping = {
  id: 'mapping', component_id: 'field', element_id: '50', element_revision_id: '501',
  review_status: 'proposed', version: 1, evidence: { reason: 'Existing evidence' },
  element_reference: { name: 'Current element', status: 'resolved', revision_no: 2 }
}
const entry = id => ({
  id, entry_status: 'active', governance_status: 'curated',
  components: [{ id: 'field', component_status: 'active', display_name: 'Sample field' }],
  standard_mappings: [mapping]
})
function mount({ componentRows, selectComponents } = {}) {
  const initialEntry = entry('first')
  if (componentRows) initialEntry.components = componentRows
  const state = reactive({ entry: initialEntry, canEdit: true })
  const controls = new Map()
  const root = document.createElement('div')
  document.body.appendChild(root)
  const app = createApp({ render: () => h(StandardMappingPanel, state) })
  app.component('el-dialog', { props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue
    ? h('section', { 'data-dialog': '' }, [...(slots.default?.() || []), ...(slots.footer?.() || [])]) : null })
  app.component('el-card', { setup: (_, { slots }) => () => h('div', [...(slots.header?.() || []), ...(slots.default?.() || [])]) })
  app.component('el-table-column', { setup: (_, { slots }) => () => h('div', slots.default?.({ row: state.entry.standard_mappings[0] })) })
  app.component('el-select', selectComponents?.ElSelect || { setup: (_, { attrs, slots }) => () => {
    controls.set(attrs.placeholder, attrs)
    return h('div', { 'data-select': attrs.placeholder }, slots.default?.())
  } })
  app.component('el-option', selectComponents?.ElOption || { props: ['label'], setup: props => () => h('span', props.label) })
  app.component('el-button', { setup: (_, { attrs, slots }) => () => h('button', attrs, slots.default?.()) })
  for (const name of ['table', 'empty', 'tag', 'form', 'form-item', 'input', 'input-number']) {
    app.component(`el-${name}`, { setup: (_, { slots }) => () => h('div', slots.default?.()) })
  }
  app.mount(root)
  mounted.push({ app, root })
  return {
    state, root,
    click: key => [...root.querySelectorAll('button')].find(node => node.textContent === key).click(),
    select: kind => controls.get(`catalog.mapping.${kind}Placeholder`),
    options: kind => root.querySelector(`[data-select="catalog.mapping.${kind}Placeholder"]`)?.textContent || '',
    open: () => Boolean(root.querySelector('[data-dialog]')),
    unmount: () => { app.unmount(); root.remove(); mounted.splice(mounted.findIndex(item => item.app === app), 1) }
  }
}
beforeEach(() => {
  vi.resetAllMocks()
  api.listReferenceCandidates.mockResolvedValue({ data: [] })
  api.listStandardMappingRevisionOptions.mockResolvedValue([])
})
afterEach(() => mounted.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() }))

describe('standard mapping candidate query lifecycle', () => {
  it.each(['replace', 'mutate'])('updates a real open field select when %s fills initially empty components', async change => {
    const { ElSelect, ElOption } = await vi.importActual('element-plus')
    const view = mount({ componentRows: [], selectComponents: { ElSelect, ElOption } })
    view.click('catalog.mapping.propose'); await settle()
    const wrapper = view.root.querySelector('[data-dialog] .el-select__wrapper')
    wrapper.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await settle()
    const input = wrapper.querySelector('[role="combobox"]')
    expect(input.getAttribute('aria-expanded')).toBe('true')
    const list = document.getElementById(input.getAttribute('aria-controls'))
    expect(list.querySelectorAll('[role="option"]')).toHaveLength(0)

    const fields = ['person_id', 'person_nickname', 'activity_id'].map(id => ({ id, display_name: id, component_status: 'active' }))
    fields.push({ id: 'old', display_name: 'Retired field', component_status: 'missing' })
    if (change === 'replace') view.state.entry = { ...entry('first'), components: fields }
    else view.state.entry.components.push(...fields)
    await settle()
    expect(view.open()).toBe(true)
    expect(input.getAttribute('aria-expanded')).toBe('true')
    const options = [...list.querySelectorAll('[role="option"]')]
    expect(options.map(node => node.textContent)).toEqual(['person_id', 'person_nickname', 'activity_id'])
    expect(list.textContent).not.toContain('Retired field')
    options[0].dispatchEvent(new MouseEvent('click', { bubbles: true })); await settle()
    expect(wrapper.textContent).toContain('person_id')
    expect(api.createStandardMapping).not.toHaveBeenCalled()
    expect(api.updateStandardMapping).not.toHaveBeenCalled()
  })

  it('does not reuse an earlier dialog element search after reopening', async () => {
    const pending = deferred()
    api.listReferenceCandidates.mockReturnValue(pending.promise)
    const view = mount()
    view.click('catalog.mapping.propose'); await settle()
    const request = view.select('element')['remote-method']('old')
    view.click('catalog.edit.cancel'); await settle()
    view.click('catalog.mapping.propose'); await settle()
    pending.resolve({ data: [{ id: 'old', name: 'Old element' }] })
    await request; await settle()
    expect(view.options('element')).not.toContain('Old element')
    expect(view.select('element').loading).toBe(false)
  })

  it('does not restore an earlier mapping revision into a new blank candidate', async () => {
    const pending = deferred()
    api.listStandardMappingRevisionOptions.mockReturnValue(pending.promise)
    const view = mount()
    view.click('catalog.mapping.edit'); await settle()
    expect(api.listStandardMappingRevisionOptions).toHaveBeenCalledWith('50')
    view.click('catalog.edit.cancel'); await settle()
    view.click('catalog.mapping.propose'); await settle()
    pending.resolve([{ id: '501', revision_no: 2, name: 'Old revision' }]); await settle()
    expect(view.options('revision')).not.toContain('Old revision')
    expect(view.select('revision').modelValue).toBe('')
    expect(view.select('revision').loading).toBe(false)
  })

  it('stops revision loading when the selected element is cleared', async () => {
    const pending = deferred()
    api.listStandardMappingRevisionOptions.mockReturnValue(pending.promise)
    const view = mount()
    view.click('catalog.mapping.edit'); await settle()
    view.select('element')['onUpdate:modelValue']('')
    await view.select('element').onChange(); await settle()
    pending.resolve([{ id: '501', revision_no: 2, name: 'Old revision' }]); await settle()
    expect(view.options('revision')).toBe('')
    expect(view.select('revision').loading).toBe(false)
    expect(view.select('revision').disabled).toBe(true)
  })

  it.each(['close', 'unmount'])('ignores a late query error after %s', async action => {
    const pending = deferred()
    api.listReferenceCandidates.mockReturnValue(pending.promise)
    const view = mount()
    view.click('catalog.mapping.propose'); await settle()
    const request = view.select('element')['remote-method']('old')
    if (action === 'close') view.click('catalog.edit.cancel')
    else view.unmount()
    await settle(); pending.reject(new Error('old query failed'))
    await request; await settle()
    expect(api.error).not.toHaveBeenCalled()
  })

  it('closes the old candidate when the Catalog entry changes', async () => {
    const view = mount()
    view.click('catalog.mapping.propose'); await settle()
    view.state.entry = entry('second'); await settle()
    expect(view.open()).toBe(false)
  })

  it('preserves the selected published revision and reports current query failures', async () => {
    api.listStandardMappingRevisionOptions.mockResolvedValue([{ id: '501', revision_no: 2, name: 'Published revision' }])
    const view = mount()
    view.click('catalog.mapping.edit'); await settle()
    expect(view.select('revision').modelValue).toBe('501')
    expect(view.options('revision')).toContain('R2 · Published revision')
    api.listReferenceCandidates.mockRejectedValue(new Error('current query failed'))
    await view.select('element')['remote-method']('new')
    expect(api.error).toHaveBeenCalledExactlyOnceWith('catalog.mapping.loadFailed')
  })
})
