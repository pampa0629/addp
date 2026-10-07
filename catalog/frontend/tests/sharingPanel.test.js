// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, createRenderer, h, inject, nextTick, provide, reactive, ref, ssrContextKey } from 'vue'
import SharingPanel from '../src/components/SharingPanel.vue'

const fixtures = vi.hoisted(() => ({ auth: null, api: Object.fromEntries([
  'createSharingDecision', 'getSharingDecision', 'listSharingRecipients', 'listSharingRequests', 'getSharingRequest',
  'listSharingDecisions', 'observeSharingRequirement', 'prepareSharingRequest', 'initializeSharingRequirement', 'listSharingConfirmations', 'listSharingConfirmationResults'
].map(name => [name, vi.fn()])) }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => fixtures.auth }))
vi.mock('../src/api/catalog', () => fixtures.api)
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key, locale: ref('en') }) }))

// Mount the actual SFC with Vue's renderer, replacing only Element Plus host
// controls. This exercises watchers/commands, not source-string assertions.
const renderer = createRenderer({
  createElement: tag => ({ tag, props: {}, children: [], parent: null }),
  createText: text => ({ text, children: [] }), createComment: text => ({ text, children: [] }),
  setText: (node, text) => { node.text = text }, setElementText: (node, text) => { node.text = text; node.children = [] },
  patchProp: (node, key, old, value) => { node.props[key] = value },
  insert(node, parent, anchor) {
    if (node.parent) { const previous = node.parent.children.indexOf(node); if (previous >= 0) node.parent.children.splice(previous, 1) }
    node.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, node)
  },
  remove(node) { const index = node.parent?.children.indexOf(node); if (index >= 0) node.parent.children.splice(index, 1); node.parent = null },
  parentNode: node => node.parent, nextSibling: node => node.parent?.children[node.parent.children.indexOf(node) + 1],
})
const controls = ['alert', 'card', 'form', 'form-item', 'select', 'option', 'date-picker', 'input', 'button', 'descriptions', 'descriptions-item', 'pagination', 'empty']
const rowContext = Symbol('table-row')
const mounted = []
const id = 'cc0a8000-6000-4000-8000-800000000001'
const decision = { id: 'cc0a8000-6000-4000-8000-800000000002', recipient_type: 'user', recipient_id: '60', recipient_name: 'B',
  expiry_mode: 'until_revoked', target: { engine_id: '9007199254740993', version: 'v1', segments: [{ name: 'root' }, { name: 'table' }] } }
const entry = { id: 'entry', version: 4, entry_type: 'data_item', entry_status: 'active', governance_status: 'curated',
  source: { source_module: 'meta', source_status: 'active' },
  responsibilities: [{ role: 'business_owner', subject_type: 'user', subject_id: '50', status: 'active' }] }
const flatten = node => [node, ...node.children.flatMap(flatten)]
async function settle() { for (let i = 0; i < 5; i++) { await Promise.resolve(); await nextTick() } }
function mount(decisionID = '', requestID = '') {
  const state = reactive({ entry, decisionID, requestID })
  const recordDecision = vi.fn(async value => { state.decisionID = value })
  const recordRequest = vi.fn(async value => { state.requestID = value })
  const root = { children: [] }
  const app = renderer.createApp({ render: () => h(SharingPanel, { ...state, recordDecision, recordRequest }) })
  app.provide(ssrContextKey, { modules: new Set() })
  for (const name of controls) app.component(`el-${name}`, { inheritAttrs: false, setup: (props, { attrs, slots }) => () => h(name, attrs, [...(slots.header?.() || []), ...(slots.default?.() || [])]) })
  app.component('el-table', { inheritAttrs: false, setup(props, { attrs, slots }) {
    provide(rowContext, computed(() => attrs.data?.[0]))
    return () => h('table', attrs, slots.default?.())
  } })
  app.component('el-table-column', { inheritAttrs: false, setup(props, { attrs, slots }) {
    const row = inject(rowContext)
    return () => h('table-column', attrs, row.value ? slots.default?.({ row: row.value }) : [])
  } })
  app.directive('loading', {})
  app.mount(root)
  mounted.push(app)
  return { app, state, root, recordDecision, recordRequest, control: (name, index = 0) => flatten(root).filter(node => node.tag === name)[index],
    button: testID => flatten(root).find(node => node.props?.['data-testid'] === testID), nodes: () => flatten(root) }
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('crypto', { randomUUID: () => id })
  fixtures.auth = reactive({ authContext: { principal: { type: 'user', id: '50' }, context: { tenant_id: '7', tenant_membership_id: '51' } },
    permissions: ['catalog.entry.read', 'catalog.sharing_decision.create', 'system.engine_access_fulfillment.create'],
    hasPermission(permission) { return this.permissions.includes(permission) } })
  fixtures.api.listSharingRequests.mockResolvedValue({ data: [], total: 0 })
  fixtures.api.listSharingConfirmations.mockResolvedValue({ data: [], total: 0 })
  fixtures.api.listSharingConfirmationResults.mockResolvedValue({ data: [], total: 0 })
  fixtures.api.listSharingRecipients.mockResolvedValue({ data: [{ id: '60', recipient_type: 'user', name: 'B', status: 'active' }] })
  fixtures.api.listSharingDecisions.mockResolvedValue({ data: [decision], total: 1 })
  fixtures.api.observeSharingRequirement.mockResolvedValue({ mode: 'catalog', requirement_version: '9007199254740993' })
})
afterEach(() => { mounted.splice(0).forEach(app => app.unmount()); vi.unstubAllGlobals() })

describe('actual sharing panel commands and recovery', () => {
  it('finds confirmation history from an ordinary entry URL and opens authoritative results without fulfillment permission or a new command', async () => {
    fixtures.auth.permissions = ['catalog.entry.read', 'catalog.sharing_decision.create']
    fixtures.api.listSharingConfirmations.mockResolvedValue({ data: [decision], total: 21 })
    fixtures.api.getSharingDecision.mockResolvedValue(decision)
    fixtures.api.listSharingConfirmationResults.mockResolvedValue({ data: [{ request_id: id, decision_id: decision.id, state: 'accepted', granted_at: '2026-10-07T04:00:00Z' }], total: 1 })
    const view = mount(); await settle()
    expect(fixtures.api.listSharingConfirmations).toHaveBeenCalledWith('entry', { page: 1, page_size: 20 })
    await view.button('sharing-open-confirmation').props.onClick(); await settle()
    expect(view.recordDecision).toHaveBeenCalledWith(decision.id)
    expect(fixtures.api.getSharingDecision).toHaveBeenCalledWith('entry', decision.id)
    expect(fixtures.api.listSharingConfirmationResults).toHaveBeenCalledWith('entry', decision.id, { page: 1, page_size: 20 })
    expect(view.button('sharing-prepare')).toBeUndefined()
    expect(view.nodes().find(node => node.props?.['data-testid'] === 'sharing-results').props.data[0].granted_at).toBeTruthy()
    const paging = view.nodes().find(node => node.props?.['data-testid'] === 'sharing-confirmation-pagination')
    await paging.props.onCurrentChange(2); await settle()
    expect(fixtures.api.listSharingConfirmations).toHaveBeenLastCalledWith('entry', { page: 2, page_size: 20 })
    expect(fixtures.api.createSharingDecision).not.toHaveBeenCalled()
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
    await view.button('sharing-new-confirmation').props.onClick(); await settle()
    expect(view.state.decisionID).toBe('')
    expect(view.button('sharing-confirm')).toBeDefined()
  })
  it('clears issuance results on refresh failure instead of claiming no request or current access', async () => {
    fixtures.api.getSharingDecision.mockResolvedValue(decision)
    fixtures.api.listSharingConfirmationResults.mockResolvedValueOnce({ data: [{ request_id: id, decision_id: decision.id, state: 'accepted', granted_at: '2026-10-07T04:00:00Z' }], total: 1 }).mockRejectedValue(new Error('offline'))
    const view = mount(decision.id); await settle()
    await view.button('sharing-refresh-results').props.onClick(); await settle()
    expect(view.nodes().find(node => node.props?.['data-testid'] === 'sharing-results').props.data).toEqual([])
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.queryFailed')).toBe(true)
    expect(view.nodes().some(node => node.props?.description === 'catalog.sharing.noResults')).toBe(false)
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
  })
  it('ignores old confirmation lists and results after confirmation permission is withdrawn', async () => {
    let resolveList, resolveResults
    fixtures.api.listSharingConfirmations.mockReturnValueOnce(new Promise(done => { resolveList = done })).mockResolvedValue({ data: [], total: 0 })
    fixtures.api.getSharingDecision.mockResolvedValue(decision)
    fixtures.api.listSharingConfirmationResults.mockReturnValue(new Promise(done => { resolveResults = done }))
    const view = mount(decision.id); await settle()
    fixtures.auth.permissions = []; await settle()
    resolveList({ data: [decision], total: 1 }); resolveResults({ data: [{ decision_id: decision.id, state: 'accepted' }], total: 1 }); await settle()
    expect(view.nodes().some(node => node.props?.['data-testid'] === 'sharing-confirmations' || node.props?.['data-testid'] === 'sharing-results')).toBe(false)
    expect(fixtures.api.createSharingDecision).not.toHaveBeenCalled()
  })
  const decisionDropdown = view => view.nodes().find(node => node.tag === 'select' && node.props.placeholder === 'catalog.sharing.selectDecision')
  async function selectDecision(view) {
    const dropdown = decisionDropdown(view)
    await dropdown.props.onVisibleChange(true); await settle()
    dropdown.props['onUpdate:modelValue'](decision.id); await settle()
    await dropdown.props.onChange(); await settle()
  }
  it('requires independent initialization permission and an explicit reason, then reobserves before enabling preparation', async () => {
    fixtures.auth.permissions.push('system.engine_access_approval_requirement.initialize')
    fixtures.api.observeSharingRequirement.mockRejectedValueOnce({ response: { status: 404, data: { error: 'Not found' } } })
      .mockResolvedValue({ mode: 'catalog', requirement_version: '8' })
    fixtures.api.initializeSharingRequirement.mockResolvedValue({ version: 1 })
    const view = mount(); await settle(); await selectDecision(view)
    expect(fixtures.api.initializeSharingRequirement).not.toHaveBeenCalled()
    await view.button('sharing-initialize').props.onClick(); await settle()
    expect(fixtures.api.initializeSharingRequirement).not.toHaveBeenCalled()
    view.button('sharing-initialize-reason').props['onUpdate:modelValue']('Configure approval')
    await view.button('sharing-initialize').props.onClick(); await settle()
    expect(fixtures.api.initializeSharingRequirement).toHaveBeenCalledTimes(1)
    expect(fixtures.api.initializeSharingRequirement.mock.calls[0][1]).toContain('"engine_id":9007199254740993,')
    expect(fixtures.api.observeSharingRequirement).toHaveBeenCalledTimes(2)
    expect(view.button('sharing-prepare').props.disabled).toBe(false)
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
  })
  it('never initializes automatically on 403 or network failure and hides initialization without its separate permission', async () => {
    fixtures.api.observeSharingRequirement.mockRejectedValue({ response: { status: 403 } })
    const view = mount(); await settle(); await selectDecision(view)
    expect(view.button('sharing-initialize')).toBeUndefined()
    expect(fixtures.api.initializeSharingRequirement).not.toHaveBeenCalled()
    expect(view.button('sharing-prepare').props.disabled).toBe(true)
  })
  it('retains configuration failure without preparing or automatically retrying a conflicting initialization', async () => {
    fixtures.auth.permissions.push('system.engine_access_approval_requirement.initialize')
    fixtures.api.observeSharingRequirement.mockRejectedValue({ response: { status: 404 } })
    fixtures.api.initializeSharingRequirement.mockRejectedValue({ response: { status: 409, data: { error: 'Already configured' } } })
    const view = mount(); await settle(); await selectDecision(view)
    view.button('sharing-initialize-reason').props['onUpdate:modelValue']('Configure approval')
    await view.button('sharing-initialize').props.onClick(); await settle()
    expect(fixtures.api.initializeSharingRequirement).toHaveBeenCalledTimes(1)
    expect(view.button('sharing-initialize-reason').props.modelValue).toBe('Configure approval')
    expect(view.button('sharing-prepare').props.disabled).toBe(true)
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
  })
  it('allows a separately qualified handler to prepare without business-owner or confirmation permission', async () => {
    fixtures.auth.authContext.principal.id = '70'
    fixtures.auth.permissions = ['catalog.entry.read', 'system.engine_access_fulfillment.create']
    fixtures.api.prepareSharingRequest.mockResolvedValue({ request_id: id, state: 'accepted' })
    fixtures.api.getSharingRequest.mockResolvedValue({ request_id: id, state: 'accepted', target: decision.target })
    const view = mount(); await settle(); await selectDecision(view)
    expect(view.button('sharing-confirm')).toBeUndefined()
    await view.button('sharing-prepare').props.onClick(); await settle()
    expect(fixtures.api.prepareSharingRequest).toHaveBeenCalledTimes(1)
    expect(fixtures.api.createSharingDecision).not.toHaveBeenCalled()
  })
  it('observes the chosen target and freezes the original request before explicit POST and same-parameter retry', async () => {
    fixtures.api.prepareSharingRequest.mockRejectedValueOnce(new Error('transport failed')).mockResolvedValueOnce({ request_id: id, state: 'accepted' })
    fixtures.api.getSharingRequest.mockResolvedValueOnce({ request_id: id, state: 'pending', target: decision.target })
      .mockResolvedValue({ request_id: id, state: 'accepted', target: decision.target })
    const view = mount(); await settle(); await selectDecision(view)
    expect(fixtures.api.listSharingDecisions).toHaveBeenCalledWith('entry', { page: 1, page_size: 20 })
    expect(fixtures.api.observeSharingRequirement).toHaveBeenCalledWith(decision.target)
    expect(view.button('sharing-prepare').props.disabled).toBe(false)
    const submit = view.button('sharing-prepare').props.onClick
    await Promise.all([submit(), submit()]); await settle()
    expect(view.recordRequest).toHaveBeenCalledExactlyOnceWith(id)
    expect(view.state.requestID).toBe(id)
    expect(fixtures.api.prepareSharingRequest).toHaveBeenCalledTimes(1)
    const payload = fixtures.api.prepareSharingRequest.mock.calls[0][1]
    expect(payload).toEqual({ request_id: id, decision_id: decision.id, requirement_version: '9007199254740993' })
    expect(Object.isFrozen(payload)).toBe(true)
    await view.button('sharing-query-request').props.onClick(); await settle()
    expect(fixtures.api.prepareSharingRequest).toHaveBeenCalledTimes(1)
    await view.button('sharing-retry-request').props.onClick(); await settle()
    expect(fixtures.api.prepareSharingRequest.mock.calls[1][1]).toBe(payload)
    expect(fixtures.api.observeSharingRequirement).toHaveBeenCalledTimes(1)
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.state.accepted')).toBe(true)
    expect(view.button('sharing-retry-request')).toBeUndefined()
  })
  it('retains a pending original command for explicit retry and stops retrying when GET finds acceptance', async () => {
    fixtures.api.prepareSharingRequest.mockResolvedValue({ request_id: id, state: 'pending' })
    fixtures.api.getSharingRequest.mockResolvedValueOnce({ request_id: id, state: 'pending', target: decision.target })
      .mockResolvedValue({ request_id: id, state: 'accepted', target: decision.target })
    const view = mount(); await settle(); await selectDecision(view)
    await view.button('sharing-prepare').props.onClick(); await settle()
    expect(view.button('sharing-retry-request')).toBeDefined()
    await view.button('sharing-query-request').props.onClick(); await settle()
    expect(view.button('sharing-retry-request')).toBeUndefined()
    expect(fixtures.api.prepareSharingRequest).toHaveBeenCalledTimes(1)
  })
  it('drops the captured command when public history switches to another request', async () => {
    fixtures.api.prepareSharingRequest.mockRejectedValue(new Error('transport failed'))
    const view = mount(); await settle(); await selectDecision(view)
    await view.button('sharing-prepare').props.onClick(); await settle()
    expect(view.button('sharing-retry-request')).toBeDefined()
    view.state.requestID = decision.id; await settle()
    expect(view.button('sharing-retry-request')).toBeUndefined()
    expect(fixtures.api.prepareSharingRequest).toHaveBeenCalledTimes(1)
  })
  it('restores the original routed request using GET only even if lookup fails', async () => {
    fixtures.api.getSharingRequest.mockRejectedValue(new Error('offline'))
    const view = mount('', id); await settle()
    expect(fixtures.api.getSharingRequest).toHaveBeenCalledWith('entry', id)
    expect(fixtures.api.listSharingDecisions).not.toHaveBeenCalled()
    expect(fixtures.api.observeSharingRequirement).not.toHaveBeenCalled()
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
    expect(view.button('sharing-prepare')).toBeUndefined()
  })
  it('does not display another request as the original authoritative outcome', async () => {
    fixtures.api.getSharingRequest.mockResolvedValue({ request_id: decision.id, state: 'accepted', target: decision.target })
    const view = mount('', id); await settle()
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.state.accepted')).toBe(false)
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.queryFailed')).toBe(true)
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
  })
  it('ignores an old requirement response after handling permission is withdrawn', async () => {
    let resolve
    fixtures.api.observeSharingRequirement.mockReturnValue(new Promise(done => { resolve = done }))
    const view = mount(); await settle()
    const dropdown = view.control('select', 3)
    await dropdown.props.onVisibleChange(true); await settle()
    dropdown.props['onUpdate:modelValue'](decision.id); await settle()
    const pending = dropdown.props.onChange(); await settle()
    fixtures.auth.permissions = ['catalog.entry.read', 'catalog.sharing_decision.create']; await settle()
    resolve({ mode: 'catalog', requirement_version: '1' }); await pending; await settle()
    expect(view.button('sharing-prepare')).toBeUndefined()
    expect(view.nodes().some(node => node.tag === 'table' && !node.props['data-testid'])).toBe(false)
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
  })
  it.each([{ mode: 'independent', requirement_version: '1' }, { mode: 'catalog', requirement_version: 1 }, { mode: 'catalog', requirement_version: '0' }])('rejects an unusable requirement without a new command: %j', async requirement => {
    fixtures.api.observeSharingRequirement.mockResolvedValue(requirement)
    const view = mount(); await settle(); await selectDecision(view)
    expect(view.button('sharing-prepare').props.disabled).toBe(true)
    await view.button('sharing-prepare').props.onClick(); await settle()
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.invalidRequirement')).toBe(true)
  })
  it('invalidates a late observation when selection clears', async () => {
    let resolve
    fixtures.api.observeSharingRequirement.mockReturnValue(new Promise(done => { resolve = done }))
    const view = mount(); await settle()
    const dropdown = view.control('select', 3)
    await dropdown.props.onVisibleChange(true); await settle()
    dropdown.props['onUpdate:modelValue'](decision.id); await settle()
    const pending = dropdown.props.onChange(); await settle()
    dropdown.props['onUpdate:modelValue'](''); await settle(); await dropdown.props.onChange()
    resolve({ mode: 'catalog', requirement_version: '1' }); await pending; await settle()
    expect(view.button('sharing-prepare').props.disabled).toBe(true)
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
  })
  it('does not send a command after identity changes while recording the public request ID', async () => {
    let resolve
    const view = mount(); await settle(); await selectDecision(view)
    view.recordRequest.mockImplementation(() => new Promise(done => { resolve = done }))
    const pending = view.button('sharing-prepare').props.onClick(); await settle()
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
    fixtures.auth.authContext.principal.id = '70'; await settle()
    resolve(); await pending; await settle()
    expect(fixtures.api.prepareSharingRequest).not.toHaveBeenCalled()
    expect(view.button('sharing-retry-request')).toBeUndefined()
  })
  it('loads defaults on recipient dropdown and freezes the original command for explicit retry', async () => {
    fixtures.api.createSharingDecision.mockRejectedValueOnce(new Error('transport failed')).mockResolvedValueOnce({ id, target: { segments: [] }, expiry_mode: 'until_revoked' })
    const view = mount(); await settle()
    await view.control('select', 1).props.onVisibleChange(true); await settle()
    expect(fixtures.api.listSharingRecipients).toHaveBeenCalledWith('entry', { recipient_type: 'user', search: '', page: 1, page_size: 50 })
    view.control('select', 1).props['onUpdate:modelValue']('60')
    view.control('select', 2).props['onUpdate:modelValue']('until_revoked')
    view.control('input').props['onUpdate:modelValue']('Research')
    const submit = view.button('sharing-confirm').props.onClick
    await Promise.all([submit(), submit()]); await settle()
    const payload = fixtures.api.createSharingDecision.mock.calls[0][1]
    expect(view.state.decisionID).toBe(id)
    expect(Object.isFrozen(payload)).toBe(true)
    expect(fixtures.api.createSharingDecision).toHaveBeenCalledTimes(1)
    fixtures.api.getSharingDecision.mockResolvedValue({ id, target: { segments: [] } })
    await view.button('sharing-query-confirmation').props.onClick(); await settle()
    expect(fixtures.api.createSharingDecision).toHaveBeenCalledTimes(1)
    // A failed transport retry is explicit; it uses the identical captured DTO.
    fixtures.api.getSharingDecision.mockRejectedValue(new Error('offline'))
    await view.button('sharing-query-confirmation').props.onClick(); await settle()
    await view.button('sharing-retry').props.onClick(); await settle()
    expect(fixtures.api.createSharingDecision.mock.calls[1][1]).toBe(payload)
  })
  it('restores a routed original confirmation using GET only, even when lookup fails', async () => {
    fixtures.api.getSharingDecision.mockRejectedValue(new Error('offline'))
    const view = mount(id); await settle()
    expect(fixtures.api.getSharingDecision).toHaveBeenCalledWith('entry', id)
    expect(fixtures.api.createSharingDecision).not.toHaveBeenCalled()
    expect(view.button('sharing-confirm')).toBeUndefined()
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.queryFailed')).toBe(true)
  })
  it('clears old requests on withdrawal and ignores an outstanding response', async () => {
    let resolve
    fixtures.api.listSharingRequests.mockReturnValue(new Promise(done => { resolve = done }))
    const view = mount(); await settle()
    fixtures.auth.permissions = []; await settle()
    resolve({ data: [{ request_id: 'must-not-show' }], total: 1 }); await settle()
    expect(view.nodes().some(node => node.tag === 'table')).toBe(false)
    expect(fixtures.api.createSharingDecision).not.toHaveBeenCalled()
  })
  it('queries the original outcome with GET and never turns a dependency failure into pending', async () => {
    fixtures.api.listSharingRequests.mockResolvedValue({ data: [{ request_id: 'request' }], total: 1 })
    fixtures.api.getSharingRequest.mockResolvedValueOnce({ request_id: 'request', state: 'accepted', target: { segments: [] } }).mockRejectedValueOnce(new Error('offline'))
    const view = mount(); await settle()
    const query = view.nodes().find(node => node.tag === 'button' && node.children.some(child => child.text === 'catalog.sharing.queryResult'))
    await query.props.onClick(); await settle()
    expect(fixtures.api.getSharingRequest).toHaveBeenCalledWith('entry', 'request')
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.state.accepted')).toBe(true)
    await query.props.onClick(); await settle()
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.state.accepted')).toBe(false)
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.state.pending')).toBe(false)
    expect(view.nodes().some(node => node.props?.title === 'catalog.sharing.queryFailed')).toBe(true)
    expect(fixtures.api.createSharingDecision).not.toHaveBeenCalled()
  })
})
