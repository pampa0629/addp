// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import EntryGovernanceDialog from '../src/components/EntryGovernanceDialog.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), error: vi.fn() }))
vi.mock('../src/api/catalog', () => ({ listEntries: mocks.list }))
vi.mock('element-plus', () => ({ ElMessage: { error: mocks.error } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))

const mounted = []
const entry = id => ({ id, recommended_successor: { id: `${id}-successor`, display_name: `${id} successor` } })
const settle = async () => { for (let i = 0; i < 4; i++) { await Promise.resolve(); await nextTick() } }
function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function mount() {
  const state = reactive({ visible: true, entry: entry('first'), mode: 'deprecate' })
  const root = document.createElement('div')
  document.body.appendChild(root)
  let select
  const app = createApp({ render: () => h(EntryGovernanceDialog, state) })
  app.component('el-dialog', { props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('div', slots.default?.()) : null })
  app.component('el-select', { setup: (_, { attrs, slots }) => () => { select = attrs; return h('div', slots.default?.()) } })
  app.component('el-option', { props: ['label'], setup: props => () => h('span', { 'data-option': '' }, props.label) })
  for (const name of ['alert', 'form', 'form-item', 'input', 'button']) {
    app.component(`el-${name}`, { setup: (_, { slots }) => () => h('div', slots.default?.()) })
  }
  app.mount(root)
  mounted.push({ app, root })
  return { state, app, options: () => [...root.querySelectorAll('[data-option]')].map(node => node.textContent),
    search: query => select['remote-method'](query), loading: () => select.loading,
    unmount: () => { app.unmount(); root.remove(); mounted.splice(mounted.findIndex(item => item.app === app), 1) } }
}
beforeEach(() => vi.resetAllMocks())
afterEach(() => mounted.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() }))

describe('governance successor query lifecycle', () => {
  it('discards candidates from a dialog closed and reopened for another entry', async () => {
    const old = deferred()
    mocks.list.mockReturnValue(old.promise)
    const view = mount()
    const search = view.search('old')
    await settle()
    expect(view.loading()).toBe(true)
    view.state.visible = false
    await settle()
    view.state.entry = entry('second')
    view.state.visible = true
    await settle()
    old.resolve({ data: [{ id: 'old-result', display_name: 'Old result' }] })
    await search
    await settle()
    expect(view.options()).toEqual(['second successor'])
    expect(view.loading()).toBe(false)
  })

  it.each(['close', 'change-mode', 'unmount'])('ignores a late search error after %s', async action => {
    const old = deferred()
    mocks.list.mockReturnValue(old.promise)
    const view = mount()
    const search = view.search('old')
    if (action === 'close') view.state.visible = false
    else if (action === 'change-mode') view.state.mode = 'withdraw-certification'
    else view.unmount()
    await settle()
    old.reject(new Error('late error'))
    await search
    await settle()
    expect(mocks.error).not.toHaveBeenCalled()
  })

  it('keeps only the latest query and still reports current errors', async () => {
    const old = deferred(), latest = deferred()
    mocks.list.mockReturnValueOnce(old.promise).mockReturnValueOnce(old.promise)
      .mockReturnValueOnce(latest.promise).mockReturnValueOnce(latest.promise)
    const view = mount()
    const firstSearch = view.search('old')
    const latestSearch = view.search('new')
    latest.resolve({ data: [{ id: 'new-result', display_name: 'New result' }] })
    await latestSearch
    old.resolve({ data: [{ id: 'old-result', display_name: 'Old result' }] })
    await firstSearch
    await settle()
    expect(view.options()).toEqual(['first successor', 'New result'])
    mocks.list.mockRejectedValue(new Error('current error'))
    await view.search('failed')
    expect(mocks.error).toHaveBeenCalledExactlyOnceWith('catalog.governance.successorSearchFailed')
  })
})
