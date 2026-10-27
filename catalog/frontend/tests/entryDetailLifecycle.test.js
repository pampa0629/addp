// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive, ref } from 'vue'
import EntryDetail from '../src/views/EntryDetail.vue'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

const api = vi.hoisted(() => ({
  route: null, auth: null, locale: null, translate: null, router: { replace: vi.fn(), push: vi.fn() },
  getEntry: vi.fn(), getMyEntryMarks: vi.fn(), getEntryHistory: vi.fn(), replaceMyEntryMarks: vi.fn(),
  error: vi.fn(), success: vi.fn()
}))
vi.mock('vue-router', () => ({ useRoute: () => api.route, useRouter: () => api.router }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (...args) => api.translate(...args), locale: api.locale }) }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => api.auth }))
vi.mock('../src/api/catalog', () => api)
vi.mock('../src/api/client', () => ({ default: {} }))
vi.mock('../src/api/standard', () => ({ getMeasurementUnit: vi.fn(), listMetricCategories: vi.fn() }))
vi.mock('element-plus', () => ({ ElMessage: { error: api.error, success: api.success }, ElMessageBox: {} }))
vi.mock('@common-ui', async () => ({
  safeStringify: (await import('@common-ui/utils/formatters')).safeStringify,
  navigateConsoleModuleRoute: vi.fn(), openConsoleRoute: vi.fn(), allowsConsoleRoute: () => false,
  resolveConsoleRouteUrl: () => '', useConsolePageDescriptor: vi.fn(),
  resolveCanonicalTabRouteState: ({ routeQuery }) => ({ tab: routeQuery.tab || 'overview', query: routeQuery, changed: false })
}))
vi.mock('@addp/common-frontend/graph/lineageApi.js', () => ({ createLineageApi: () => ({}), normalizeLineageGraph: () => ({ nodes: [], edges: [] }) }))
vi.mock('../src/components/EntryEditor.vue', () => ({ default: { render: () => null } }))
vi.mock('../src/components/EntryGovernanceDialog.vue', () => ({ default: { render: () => null } }))
vi.mock('../src/components/StandardMappingPanel.vue', () => ({ default: { render: () => null } }))
vi.mock('../src/components/SharingPanel.vue', () => ({ default: { render: () => null } }))

const mounted = []
const settle = async () => { for (let i = 0; i < 12; i++) { await Promise.resolve(); await nextTick() } }
function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const entry = id => ({ id, display_name: `Resource ${id}`, entry_type: 'data_application', entry_status: 'active', governance_status: 'curated' })
const history = label => ({ source_bindings: [], audit_events: [{ id: label, event_type: 'catalog.entry.updated', details: { reason: label } }] })
function mount() {
  const root = document.createElement('div')
  document.body.appendChild(root)
  const app = createApp(EntryDetail)
  app.directive('loading', {})
  app.component('el-button', { setup: (_, { attrs, slots }) => () => h('button', { ...attrs, disabled: attrs.disabled || attrs.loading }, slots.default?.()) })
  app.component('el-card', { setup: (_, { slots }) => () => h('section', [...(slots.header?.() || []), ...(slots.default?.() || [])]) })
  app.component('el-alert', { props: ['title'], setup: (props, { slots }) => () => h('aside', [props.title, ...(slots.default?.() || [])]) })
  app.component('el-table', { props: ['data'], setup: props => () => h('pre', JSON.stringify(props.data)) })
  app.component('el-dialog', { render: () => null })
  for (const name of ['skeleton', 'result', 'text', 'tag', 'tabs', 'tab-pane', 'descriptions', 'descriptions-item', 'divider', 'empty', 'row', 'col',
    'form', 'form-item', 'input', 'input-number', 'dropdown', 'dropdown-menu', 'dropdown-item', 'timeline', 'timeline-item', 'table-column',
    'date-picker', 'select', 'option', 'tooltip']) {
    app.component(`el-${name}`, { setup: (_, { slots }) => () => h('div', slots.default?.()) })
  }
  app.mount(root)
  const record = { app, root }; mounted.push(record)
  return {
    root,
    button: key => [...root.querySelectorAll('button')].find(node => node.textContent.trim() === key),
    text: () => root.textContent,
    unmount: () => { app.unmount(); root.remove(); mounted.splice(mounted.indexOf(record), 1) }
  }
}
beforeEach(() => {
  vi.resetAllMocks()
  api.translate = key => key
  api.locale = ref('en')
  api.route = reactive({ params: { id: 'A' }, query: { tab: 'relations' }, path: '/entries/A' })
  api.auth = { hasPermission: permission => ['catalog.entry.read', 'catalog.audit.read'].includes(permission), authContext: { principal: { id: 'owner' }, context: { tenant_id: 'tenant' } } }
  api.getEntry.mockImplementation(async id => entry(id))
  api.getMyEntryMarks.mockResolvedValue({ favorite: false, following: false })
  api.getEntryHistory.mockImplementation(async id => history(`${id} history`))
  api.replaceMyEntryMarks.mockImplementation(async (_, marks) => marks)
})

describe('Catalog audit history presentation', () => {
  it('renders per-subject state changes outside technical details and switches language without rewriting evidence', async () => {
    const { createI18n } = await vi.importActual('vue-i18n')
    const missing = vi.fn()
    const { t, locale } = createI18n({ legacy: false, locale: 'zh-cn', fallbackLocale: false, messages: { 'zh-cn': zh, en }, missing }).global
    api.translate = t
    api.locale = locale
    const event = { id: 'state-change', event_type: 'catalog.responsibility.reference_state_changed', details: { changes: [
      { role: 'data_steward', subject_type: 'user', subject_id: '9007199254740993', previous_status: 'active', status: 'needs_transfer', reason: 'subject_not_found' },
      { role: 'data_steward', subject_type: 'user', subject_id: '9007199254740994', previous_status: 'needs_transfer', status: 'active', reason: 'reference_restored' }
    ] } }
    const before = JSON.stringify(event)
    api.getEntryHistory.mockResolvedValue({ source_bindings: [], audit_events: [event] })
    const view = mount(); await settle()
    const summary = view.root.querySelector('.audit-summary')
    const raw = view.root.querySelector('.audit-technical pre')
    expect(summary.textContent).toContain('有效 → 待移交；原因：主体不存在')
    expect(summary.textContent).toContain('待移交 → 有效；原因：主体已恢复为可引用')
    expect(summary.textContent).toContain('数据管理员 · 变更 1')
    expect(summary.textContent).toContain('数据管理员 · 变更 2')
    expect(summary.closest('details')).toBeNull()
    expect(view.root.querySelector('details').open).toBe(false)
    expect(JSON.parse(raw.textContent)).toEqual(event)
    const zhSummary = summary.textContent
    locale.value = 'en'; await settle()
    expect(summary.textContent).toContain('Active → Transfer required; Reason: Subject not found')
    expect(summary.textContent).toContain('Transfer required → Active; Reason: Subject is referenceable again')
    expect(view.root.querySelector('.audit-event-title').textContent).toBe('Responsibility subject status changed')
    expect(JSON.parse(raw.textContent)).toEqual(event)
    locale.value = 'zh-cn'; await settle()
    expect(summary.textContent).toBe(zhSummary)
    expect(summary.textContent).not.toContain('[object Object]')
    expect(summary.textContent).not.toContain('9007199254740993')
    expect(JSON.stringify(event)).toBe(before)
    expect(api.getEntryHistory).toHaveBeenCalledTimes(1)
    expect(missing).not.toHaveBeenCalled()
  })

  it('shows a readable event and responsibilities instead of object coercion', async () => {
    api.getEntryHistory.mockResolvedValue({ source_bindings: [], audit_events: [{
      id: 'update', event_type: 'catalog.entry.updated', details: {
        previous_governance_status: 'discovered', governance_status: 'curated', responsibility_count: 3,
        responsibilities: [
          { role: 'accountable_department', subject_type: 'department', subject_id: '10' },
          { role: 'business_owner', subject_type: 'user', subject_id: '20' },
          { role: 'data_steward', subject_type: 'user', subject_id: '30' }
        ]
      }
    }] })
    const view = mount(); await settle()
    const area = view.root.querySelector('.history-card')
    expect(area.textContent).not.toContain('[object Object]')
    expect(area.querySelector('.audit-event-title').textContent).toBe('catalog.history.events.entryUpdated')
    expect(area.querySelector('.audit-summary').textContent).toContain('catalog.edit.role.business_owner')
    expect(area.querySelector('details').open).toBe(false)
  })

  it('preserves complete nested evidence and large string identities without HTML injection', async () => {
    const event = { id: 'opaque', event_type: 'future.event', details: {
      subject_id: '9007199254740993', changes: [{ enabled: false, name: '<script>unsafe()</script>', note: null }]
    } }
    api.getEntryHistory.mockResolvedValue({ source_bindings: [], audit_events: [event] })
    const view = mount(); await settle()
    const evidence = view.root.querySelector('.history-card details pre')
    expect(evidence).not.toBeNull()
    expect(JSON.parse(evidence.textContent)).toEqual(event)
    expect(view.root.querySelector('script')).toBeNull()
  })

  it('uses a generic event title when an event is unknown or has no details', async () => {
    api.getEntryHistory.mockResolvedValue({ source_bindings: [], audit_events: [{ id: 'future', event_type: 'future.event', details: null }] })
    const view = mount(); await settle()
    expect(view.root.querySelector('.audit-event-title')?.textContent).toBe('catalog.history.events.other')
    expect(view.root.querySelector('.audit-summary')).toBeNull()
    expect(view.root.querySelector('.audit-technical').textContent).toContain('future.event')
  })
})
afterEach(() => mounted.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() }))

describe('Catalog detail request lifecycle', () => {
  it('does not apply old marks or restart history after switching entries', async () => {
    const old = deferred()
    api.getMyEntryMarks.mockImplementation(id => id === 'A' ? old.promise : Promise.resolve({ favorite: false, following: false }))
    const view = mount(); await settle()
    api.route.params.id = 'B'; await settle()
    old.resolve({ favorite: true, following: true }); await settle()
    expect(view.root.dataset.entryId || view.root.firstElementChild.dataset.entryId).toBe('B')
    expect(view.button('catalog.marks.favorite')).toBeTruthy()
    expect(view.button('catalog.marks.follow')).toBeTruthy()
    expect(api.getEntryHistory).toHaveBeenCalledTimes(1)
    expect(view.text()).toContain('B history')
  })

  it('ignores an old marks failure after a different entry has loaded', async () => {
    const old = deferred()
    api.getMyEntryMarks.mockImplementation(id => id === 'A' ? old.promise : Promise.resolve({ favorite: true, following: false }))
    const view = mount(); await settle()
    api.route.params.id = 'B'; await settle()
    old.reject(new Error('Old marks failure')); await settle()
    expect(view.button('catalog.marks.favorited')).toBeTruthy()
    expect(api.error).not.toHaveBeenCalled()
  })

  it.each(['success', 'failure'])('discards old history %s after a different entry loads', async result => {
    const old = deferred()
    api.getEntryHistory.mockImplementation(id => id === 'A' ? old.promise : Promise.resolve(history('B history')))
    const view = mount(); await settle()
    api.route.params.id = 'B'; await settle()
    expect(view.text()).toContain('B history')
    if (result === 'success') old.resolve(history('Old A history'))
    else old.reject({ response: { data: { error: 'Old history failure' } } })
    await settle()
    expect(view.text()).toContain('B history')
    expect(view.text()).not.toContain('Old A history')
    expect(view.text()).not.toContain('Old history failure')
  })

  it('keeps the newest history when navigation returns to the same entry', async () => {
    const old = deferred()
    api.getEntryHistory.mockReturnValueOnce(old.promise).mockResolvedValueOnce(history('B history')).mockResolvedValueOnce(history('New A history'))
    const view = mount(); await settle()
    api.route.params.id = 'B'; await settle()
    api.route.params.id = 'A'; await settle()
    old.resolve(history('Old A history')); await settle()
    expect(view.text()).toContain('New A history')
    expect(view.text()).not.toContain('Old A history')
  })

  it('prevents mark changes until current marks are known', async () => {
    const pending = deferred()
    api.getMyEntryMarks.mockReturnValue(pending.promise)
    const view = mount(); await settle()
    expect(view.button('catalog.marks.favorite').disabled).toBe(true)
    view.button('catalog.marks.favorite').click()
    expect(api.replaceMyEntryMarks).not.toHaveBeenCalled()
    pending.resolve({ favorite: true, following: false }); await settle()
    expect(view.button('catalog.marks.favorited').disabled).toBe(false)
  })

  it('does not replace unknown marks after a current read failure', async () => {
    api.getMyEntryMarks.mockRejectedValue(new Error('Current failure'))
    const view = mount(); await settle()
    expect(api.error).toHaveBeenCalledWith('catalog.marks.loadFailed')
    expect(view.button('catalog.marks.favorite').disabled).toBe(true)
    view.button('catalog.marks.favorite').click()
    expect(api.replaceMyEntryMarks).not.toHaveBeenCalled()
    api.getMyEntryMarks.mockResolvedValue({ favorite: false, following: false })
    view.root.querySelector('.header-actions button:last-child').click(); await settle()
    expect(view.button('catalog.marks.favorite').disabled).toBe(false)
  })

  it('does not apply a completed mark change to a later entry', async () => {
    const pending = deferred()
    api.replaceMyEntryMarks.mockReturnValue(pending.promise)
    const view = mount(); await settle()
    view.button('catalog.marks.favorite').click(); await settle()
    expect(api.replaceMyEntryMarks).toHaveBeenCalledWith('A', { favorite: true, following: false })
    api.route.params.id = 'B'; await settle()
    pending.resolve({ favorite: true, following: false }); await settle()
    expect(view.button('catalog.marks.favorite')).toBeTruthy()
    expect(api.success).not.toHaveBeenCalled()
  })

  it('does not show a mark error after the page is unmounted', async () => {
    const pending = deferred()
    api.replaceMyEntryMarks.mockReturnValue(pending.promise)
    const view = mount(); await settle()
    view.button('catalog.marks.favorite').click(); await settle()
    view.unmount(); pending.reject(new Error('Old save failure')); await settle()
    expect(api.error).not.toHaveBeenCalled()
  })

  it('does not request marks or history when entry loading finishes after unmount', async () => {
    const pending = deferred()
    api.getEntry.mockReturnValue(pending.promise)
    const view = mount(); view.unmount()
    pending.resolve(entry('A')); await settle()
    expect(api.getMyEntryMarks).not.toHaveBeenCalled()
    expect(api.getEntryHistory).not.toHaveBeenCalled()
  })

  it('removes the previous entry while the next entry request is pending', async () => {
    const pending = deferred()
    const view = mount(); await settle()
    api.getEntry.mockReturnValue(pending.promise)
    api.route.params.id = 'B'; await settle()
    expect(view.text()).not.toContain('Resource A')
    expect(view.button('catalog.marks.favorite')).toBeUndefined()
    pending.resolve(entry('B')); await settle()
    expect(view.text()).toContain('Resource B')
  })

  it('reports a current history failure and can reload its current history', async () => {
    api.getEntryHistory.mockRejectedValueOnce({ response: { data: { error: 'Current history failure' } } })
    const view = mount(); await settle()
    expect(view.text()).toContain('Current history failure')
    const refresh = [...view.root.querySelectorAll('.history-card button')].find(node => node.textContent === 'catalog.common.refresh')
    refresh.click(); await settle()
    expect(view.text()).toContain('A history')
    expect(view.text()).not.toContain('Current history failure')
  })

  it('still reports a current mark save failure', async () => {
    api.replaceMyEntryMarks.mockRejectedValue(new Error('Current save failure'))
    const view = mount(); await settle()
    view.button('catalog.marks.favorite').click(); await settle()
    expect(api.error).toHaveBeenCalledWith('catalog.marks.saveFailed')
    expect(view.button('catalog.marks.favorite').disabled).toBe(false)
  })

  it('allows known mark changes independently of a pending history read', async () => {
    const pending = deferred()
    api.getEntryHistory.mockReturnValue(pending.promise)
    const view = mount(); await settle()
    expect(view.button('catalog.marks.favorite').disabled).toBe(false)
    view.button('catalog.marks.favorite').click(); await settle()
    expect(api.replaceMyEntryMarks).toHaveBeenCalledWith('A', { favorite: true, following: false })
    pending.resolve(history('A history')); await settle()
  })
})
