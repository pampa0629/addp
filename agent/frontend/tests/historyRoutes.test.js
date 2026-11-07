import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'

const { api, network } = vi.hoisted(() => ({
  api: { list: vi.fn(), skills: vi.fn(), get: vi.fn(), getMessages: vi.fn(), workspace: vi.fn() },
  network: { hold: false, reads: [] }
}))
vi.mock('../src/api/index', () => ({ sessionAPI: api, runAPI: {}, ownerApprovalAPI: {} }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => ({
  hasPermission: permission => ['agent.session.read', 'agent.run.create', 'agent.run.execute'].includes(permission)
}) }))
vi.mock('../src/agent/createAgentClient', () => ({ createAgentClient: vi.fn(), replayAgentRunEvents: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', async () => ({
  createLatestRequestScope: (await import('../../../common-frontend/basic/src/utils/latestRequestScope.mjs')).createLatestRequestScope,
  DataPagination: { template: '<div />' }, useConsolePageDescriptor: vi.fn(),
  openConsoleRoute: vi.fn(), resolveTaskOwnerUrl: vi.fn(), takeConsoleNavigationDraft: vi.fn(),
  navigateConsoleModuleRoute: (router, module, location, options = {}) => router[options.history === 'replace' ? 'replace' : 'push'](location)
}))
import ChatView from '../src/views/ChatView.vue'

const stamp = '2026-11-08T00:00:00Z'
const message = (id, content) => ({ id, protocol_message_id: `p-${id}`, role: 'assistant', content, created_at: stamp })
const page = data => ({ data, page: { limit: 20, older_cursor: 'older', newer_cursor: null } })
function result(read) {
  if (read.kind === 'get') return { id: Number(read.id), title: `session-${read.id}`, version: 1, archived: false }
  if (read.kind === 'workspace') return { goal: `goal-${read.id}`, pending_clarifications: 0, pending_approvals: 0, clarification_id: null }
  return page([message(Number(read.params.anchor_message_id || read.id * 100), `body-${read.id}-${read.params.anchor_message_id || 'latest'}`)])
}
function read(kind, id, params, options) {
  const entry = { kind, id: String(id), params, signal: options.signal }
  network.reads.push(entry)
  if (!network.hold) return Promise.resolve(result(entry))
  // Deliberately ignore abort: the component must also reject late transport results.
  return new Promise((resolve, reject) => Object.assign(entry, { resolve, reject }))
}
let wrapper, router
const originalScroll = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
beforeAll(() => Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() }))
afterAll(() => {
  if (originalScroll) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScroll)
  else delete Element.prototype.scrollIntoView
})
async function open(path) {
  router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', name: 'Chat', component: ChatView },
    { path: '/sessions/:session_id', name: 'ChatSession', component: ChatView }
  ] })
  await router.push(path)
  await router.isReady()
  wrapper = mount(RouterView, { global: { plugins: [router], stubs: {
    'el-button': { template: '<button><slot /></button>' },
    'el-input': { props: ['type', 'disabled'], template: '<textarea v-if="type === \'textarea\'" :disabled="disabled" />' },
    'el-empty': { props: ['description'], template: '<p>{{ description }}</p>' },
    'el-dialog': true, 'el-icon': true, 'el-tag': true, 'el-tooltip': true, 'el-alert': true,
    'el-dropdown': true, 'el-dropdown-menu': true, 'el-dropdown-item': true,
    WorkspacePreferences: true, WorkspacePanel: true,
    MessagePartsRenderer: { props: ['message'], template: '<p>{{ message.content }}</p>' }
  } } })
  await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  network.hold = false; network.reads = []
  api.list.mockResolvedValue({ data: [{ id: 1, title: 'listed-other-session', updated_at: stamp }], total: 1 })
  api.skills.mockResolvedValue([])
  api.get.mockImplementation((id, options) => read('get', id, {}, options))
  api.workspace.mockImplementation((id, options) => read('workspace', id, {}, options))
  api.getMessages.mockImplementation((id, params, options) => read('messages', id, params, options))
})
afterEach(async () => {
  wrapper?.unmount(); wrapper = null
  for (const item of network.reads) item.resolve?.(result(item))
  await flushPromises()
})

describe('Agent public history routes with Vue Router and the production request scope', () => {
  it('keeps a populated home distinct and loads an unlisted deep link independently', async () => {
    await open('/')
    expect(wrapper.find('h1').text()).toBe('agent.chat.welcome')
    expect(network.reads).toEqual([])
    await router.push('/sessions/9?message_id=21'); await flushPromises()
    expect(wrapper.find('.conversation-title').text()).toBe('session-9')
    expect(wrapper.text()).toContain('body-9-21')
    expect(network.reads.map(item => item.kind)).toEqual(['get', 'messages', 'workspace'])
    expect(network.reads.every(item => item.id === '9')).toBe(true)
    expect(new Set(network.reads.map(item => item.signal)).size).toBe(1)
    expect(network.reads[1].params).toEqual({ anchor_message_id: 21 })
  })

  it('restores message_id-only route changes and excludes a late previous window', async () => {
    await open('/sessions/9?message_id=21')
    network.hold = true
    await router.push('/sessions/9?message_id=22'); await flushPromises()
    const old = network.reads.filter(item => item.resolve)
    network.hold = false
    await router.push('/sessions/9?message_id=23'); await flushPromises()
    expect(old).toHaveLength(3)
    expect(old.every(item => item.signal.aborted)).toBe(true)
    for (const item of old) item.resolve(item.kind === 'get' ? { ...result(item), title: 'stale-session', version: 99 } : result(item))
    await flushPromises()
    expect(wrapper.find('.conversation-title').text()).toBe('session-9')
    expect(wrapper.text()).toContain('body-9-23')
    expect(wrapper.text()).not.toContain('body-9-22')
  })

  it.each(['success', 'failure'])('switches identity and excludes a late previous %s', async outcome => {
    network.hold = true
    await open('/sessions/9')
    const old = [...network.reads]
    network.hold = false
    await router.push('/sessions/10'); await flushPromises()
    expect(old.every(item => item.signal.aborted)).toBe(true)
    for (const item of old) outcome === 'success' ? item.resolve(result(item)) : item.reject(new Error('old transport failure'))
    await flushPromises()
    expect(wrapper.find('.conversation-title').text()).toBe('session-10')
    expect(wrapper.text()).toContain('body-10-latest')
    expect(wrapper.text()).not.toContain('body-9-latest')
    expect(wrapper.text()).not.toContain('agent.chat.sessionUnavailable')
  })

  it('cancels the separate older-page request when the session changes', async () => {
    await open('/sessions/9')
    network.hold = true
    await wrapper.find('.history-controls button').trigger('click'); await flushPromises()
    const old = network.reads.find(item => item.params.cursor)
    network.hold = false
    await router.push('/sessions/10'); await flushPromises()
    expect(old.signal.aborted).toBe(true)
    old.resolve(page([message(899, 'stale-older-page')]))
    await flushPromises()
    expect(wrapper.text()).toContain('body-10-latest')
    expect(wrapper.text()).not.toContain('stale-older-page')
  })

  it('returns home without selecting the first list entry and disposes pending reads', async () => {
    network.hold = true
    await open('/sessions/9')
    const old = [...network.reads]
    await router.push('/'); await flushPromises()
    expect(old.every(item => item.signal.aborted)).toBe(true)
    for (const item of old) item.resolve(result(item))
    await flushPromises()
    expect(wrapper.find('h1').text()).toBe('agent.chat.welcome')
    expect(wrapper.find('.conversation-title').exists()).toBe(false)
    await router.push('/sessions/10'); await flushPromises()
    const pending = network.reads.filter(item => item.id === '10')
    wrapper.unmount(); wrapper = null
    expect(pending).toHaveLength(3)
    expect(pending.every(item => item.signal.aborted)).toBe(true)
  })
})
