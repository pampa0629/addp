import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { grantedPermissions, availableSessions, routeParams } = vi.hoisted(() => ({
  grantedPermissions: new Set(),
  availableSessions: [],
  routeParams: {}
}))

vi.mock('../src/store/auth', () => ({
  useAuthStore: () => ({ hasPermission: permission => grantedPermissions.has(permission) })
}))
vi.mock('../src/api/index', () => ({
  sessionAPI: {
    list: vi.fn(async () => availableSessions),
    getMessages: vi.fn(async () => [])
  },
  runAPI: {}
}))
vi.mock('../src/agent/createAgentClient', () => ({
  createAgentClient: vi.fn(),
  replayAgentRunEvents: vi.fn()
}))
vi.mock('@common-ui', () => ({
  useConsolePageDescriptor: vi.fn(),
  resolveTaskOwnerUrl: vi.fn()
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: routeParams }),
  useRouter: () => ({}),
  onBeforeRouteUpdate: vi.fn()
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))

import ChatView from '../src/views/ChatView.vue'
import { createAgentClient, replayAgentRunEvents } from '../src/agent/createAgentClient'

const button = { template: '<button><slot /></button>' }
const input = {
  props: ['disabled', 'modelValue'],
  emits: ['update:modelValue'],
  template: '<textarea :disabled="disabled" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
}

function renderChat() {
  return mount(ChatView, {
    global: {
      stubs: {
        'el-button': button,
        'el-input': input,
        'el-empty': true,
        'el-icon': true,
        'el-tag': true,
        'el-tooltip': true,
        MessagePartsRenderer: true
      }
    }
  })
}

describe('Agent chat operation permissions', () => {
  beforeEach(() => {
    grantedPermissions.clear()
    availableSessions.splice(0)
    delete routeParams.session_id
    vi.clearAllMocks()
  })

  it('allows an existing session to chat without run history read or session create', async () => {
    grantedPermissions.add('agent.session.read')
    grantedPermissions.add('agent.run.create')
    grantedPermissions.add('agent.run.execute')
    availableSessions.push({ id: 5, title: 'existing' })
    routeParams.session_id = '5'

    const wrapper = renderChat()
    await flushPromises()

    expect(wrapper.find('textarea').attributes('disabled')).toBeUndefined()
    expect(wrapper.findAll('button').some(item => item.text() === 'agent.chat.send')).toBe(true)
    wrapper.unmount()
  })

  it('requires session create when starting a chat without an existing session', async () => {
    grantedPermissions.add('agent.session.read')
    grantedPermissions.add('agent.run.create')
    grantedPermissions.add('agent.run.execute')

    const wrapper = renderChat()
    await flushPromises()

    expect(wrapper.find('textarea').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').some(item => item.text() === 'agent.chat.send')).toBe(false)
    wrapper.unmount()

    grantedPermissions.add('agent.session.create')
    const withCreate = renderChat()
    await flushPromises()
    expect(withCreate.find('textarea').attributes('disabled')).toBeUndefined()
    expect(withCreate.findAll('button').some(item => item.text() === 'agent.chat.send')).toBe(true)
    withCreate.unmount()
  })

  it('keeps chat creation unavailable for a read-only account', async () => {
    grantedPermissions.add('agent.session.read')
    grantedPermissions.add('agent.run.read')

    const wrapper = renderChat()
    await flushPromises()

    expect(wrapper.find('textarea').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').some(item => item.text() === 'agent.chat.send')).toBe(false)
    wrapper.unmount()
  })

  it('does not request run replay after a failed send without run read', async () => {
    grantedPermissions.add('agent.session.read')
    grantedPermissions.add('agent.run.create')
    grantedPermissions.add('agent.run.execute')
    availableSessions.push({ id: 5, title: 'existing' })
    routeParams.session_id = '5'
    createAgentClient.mockReturnValue({
      addMessage: vi.fn(),
      runAgent: vi.fn(async (_input, subscriber) => {
        subscriber.onStateSnapshotEvent({ event: { snapshot: { agentRunId: 'run-5' } } })
        throw new Error('connection lost')
      })
    })

    const wrapper = renderChat()
    await flushPromises()
    await wrapper.find('textarea').setValue('hello')
    await wrapper.findAll('button').find(item => item.text() === 'agent.chat.send').trigger('click')
    await flushPromises()

    expect(replayAgentRunEvents).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
