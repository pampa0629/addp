// @vitest-environment jsdom
import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import KnowledgeService from '../src/views/KnowledgeService.vue'

const state = vi.hoisted(() => ({ canUpdate: true, update: vi.fn(), list: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: { graph_id: '2' } }), useRouter: () => ({ resolve: () => ({ fullPath: '/knowledge-service' }) }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', () => ({ useConsolePageDescriptor: vi.fn() }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => ({ hasPermission: () => state.canUpdate }) }))
vi.mock('../src/utils/moduleNavigation', () => ({ navigateGraphRoute: vi.fn() }))
vi.mock('../src/api/knowledgeService', () => ({ knowledgeServiceApi: {
  listGraphs: state.list, updateGraph: state.update, getOntology: async () => ({ entity_types: [] }),
} }))

const Switch = { name: 'ElSwitch', props: ['modelValue', 'disabled', 'loading'], emits: ['update:modelValue', 'change'], template: '<button role="switch" :disabled="disabled || loading" :aria-checked="modelValue" @click="$emit(\'update:modelValue\', !modelValue); $emit(\'change\', !modelValue)" />' }
let wrapper
function mountService() {
  return shallowMount(KnowledgeService, { global: { renderStubDefaultSlot: true, stubs: {
    ElSwitch: Switch,
    ElTag: { template: '<span data-testid="visibility"><slot /></span>' },
  } } })
}
beforeEach(() => {
  state.canUpdate = true
  state.update.mockReset()
  state.list.mockResolvedValue([{ id: 2, name: 'test graph', is_public: false }, { id: 3, name: 'other graph', is_public: false }])
})
afterEach(() => wrapper?.unmount())

it('disables publication changes for a read-only user', async () => {
  state.canUpdate = false
  wrapper = mountService()
  await flushPromises()
  expect(wrapper.get('button[role="switch"]').element.disabled).toBe(true)
  await wrapper.get('button[role="switch"]').trigger('click')
  expect(state.update).not.toHaveBeenCalled()
})

it('uses the saved server state rather than the requested toggle value', async () => {
  state.update.mockResolvedValue({ id: 2, is_public: false })
  wrapper = mountService()
  await flushPromises()
  await wrapper.get('button[role="switch"]').trigger('click')
  await flushPromises()
  expect(state.update).toHaveBeenCalledWith(2, { is_public: true })
  expect(wrapper.get('button[role="switch"]').attributes('aria-checked')).toBe('false')
  expect(wrapper.get('[data-testid="visibility"]').text()).toBe('graph.service.private')
})

it('restores the previous saved state after an unsuccessful update', async () => {
  state.update.mockRejectedValue(new Error('update denied'))
  wrapper = mountService()
  await flushPromises()
  await wrapper.get('button[role="switch"]').trigger('click')
  await flushPromises()
  expect(wrapper.get('button[role="switch"]').attributes('aria-checked')).toBe('false')
  expect(wrapper.get('[data-testid="visibility"]').text()).toBe('graph.service.private')
})

it('applies a delayed update to its original graph after the user changes selection', async () => {
  let finish
  state.update.mockImplementation(() => new Promise(resolve => { finish = resolve }))
  wrapper = mountService()
  await flushPromises()
  await wrapper.get('button[role="switch"]').trigger('click')
  wrapper.findComponent({ name: 'ElMenu' }).vm.$emit('select', '3')
  await flushPromises()
  finish({ id: 2, is_public: true })
  await flushPromises()
  expect(wrapper.get('[data-testid="visibility"]').text()).toBe('graph.service.private')
  wrapper.findComponent({ name: 'ElMenu' }).vm.$emit('select', '2')
  await flushPromises()
  expect(wrapper.get('button[role="switch"]').attributes('aria-checked')).toBe('true')
})
