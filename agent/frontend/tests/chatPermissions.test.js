import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { grantedPermissions, availableSessions, availableMessages, routeParams } = vi.hoisted(() => ({
  grantedPermissions: new Set(),
  availableSessions: [],
  availableMessages: [],
  routeParams: {}
}))

vi.mock('../src/store/auth', () => ({
  useAuthStore: () => ({ hasPermission: permission => grantedPermissions.has(permission) })
}))
vi.mock('../src/api/index', () => ({
  sessionAPI: {
    list: vi.fn(async () => availableSessions),
    getMessages: vi.fn(async () => availableMessages)
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
    availableMessages.splice(0)
    delete routeParams.session_id
    vi.clearAllMocks()
  })

  it('resumes the pending clarification with text after loading session history', async () => {
    for (const permission of ['agent.session.read', 'agent.run.create', 'agent.run.execute']) grantedPermissions.add(permission)
    availableSessions.push({ id: 5, title: 'existing' })
    routeParams.session_id = '5'
    availableMessages.push({ role: 'assistant', parts: [
      { type: 'interaction_ref', interaction_id: 'pending-5', kind: 'clarification', status: 'pending' }
    ] })
    const addMessage = vi.fn()
    const runAgent = vi.fn(async () => {})
    createAgentClient.mockReturnValue({ addMessage, runAgent })
    const wrapper = renderChat()
    await flushPromises()
    await wrapper.find('textarea').setValue('一文档一行，只投影标量字段')
    await wrapper.findAll('button').find(item => item.text() === 'agent.chat.send').trigger('click')
    await flushPromises()
    expect(addMessage).not.toHaveBeenCalled()
    expect(runAgent.mock.calls[0][0]).toEqual({ resume: [{
      interruptId: 'pending-5', status: 'resolved', payload: { text: '一文档一行，只投影标量字段' }
    }] })
    wrapper.unmount()
  })

  it('does not guess among multiple pending clarifications or discard the answer', async () => {
    for (const permission of ['agent.session.read', 'agent.run.create', 'agent.run.execute']) grantedPermissions.add(permission)
    availableSessions.push({ id: 5, title: 'existing' })
    routeParams.session_id = '5'
    availableMessages.push({ role: 'assistant', parts: ['first', 'second'].map(interaction_id => (
      { type: 'interaction_ref', interaction_id, kind: 'clarification', status: 'pending' }
    )) })
    const runAgent = vi.fn(async () => {})
    createAgentClient.mockReturnValue({ addMessage: vi.fn(), runAgent })
    const wrapper = renderChat()
    await flushPromises()
    await wrapper.find('textarea').setValue('Outdoors')
    await wrapper.findAll('button').find(item => item.text() === 'agent.chat.send').trigger('click')
    await flushPromises()
    expect(runAgent).not.toHaveBeenCalled()
    expect(wrapper.find('textarea').element.value).toBe('Outdoors')
    wrapper.unmount()
  })

  it('keeps a text answer when the resume request fails', async () => {
    for (const permission of ['agent.session.read', 'agent.run.create', 'agent.run.execute']) grantedPermissions.add(permission)
    availableSessions.push({ id: 5, title: 'existing' })
    routeParams.session_id = '5'
    availableMessages.push({ role: 'assistant', parts: [
      { type: 'interaction_ref', interaction_id: 'pending-5', kind: 'clarification', status: 'pending' }
    ] })
    const runAgent = vi.fn(async () => { throw new Error('request rejected') })
    createAgentClient.mockReturnValue({ addMessage: vi.fn(), runAgent })
    const wrapper = renderChat()
    await flushPromises()
    await wrapper.find('textarea').setValue('一文档一行')
    await wrapper.findAll('button').find(item => item.text() === 'agent.chat.send').trigger('click')
    await flushPromises()
    expect(runAgent).toHaveBeenCalledTimes(1)
    expect(wrapper.find('textarea').element.value).toBe('一文档一行')
    wrapper.unmount()
  })

  it('does not turn completed clarification or owner approval into a text resume', async () => {
    for (const permission of ['agent.session.read', 'agent.run.create', 'agent.run.execute']) grantedPermissions.add(permission)
    availableSessions.push({ id: 5, title: 'existing' })
    routeParams.session_id = '5'
    availableMessages.push({ role: 'assistant', parts: [
      { type: 'interaction_ref', interaction_id: 'done-5', kind: 'clarification', status: 'completed' },
      { type: 'interaction_ref', interaction_id: 'approval-5', kind: 'owner_approval', status: 'pending' }
    ] })
    const addMessage = vi.fn()
    const runAgent = vi.fn(async () => {})
    createAgentClient.mockReturnValue({ addMessage, runAgent })
    const wrapper = renderChat()
    await flushPromises()
    await wrapper.find('textarea').setValue('解释一下审批流程')
    await wrapper.findAll('button').find(item => item.text() === 'agent.chat.send').trigger('click')
    await flushPromises()
    expect(addMessage).toHaveBeenCalledTimes(1)
    expect(runAgent.mock.calls[0][0]).toEqual({})
    wrapper.unmount()
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
