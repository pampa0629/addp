// @vitest-environment jsdom
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import ReviewQueue from '../src/views/ReviewQueue.vue'

const state = vi.hoisted(() => ({ list: vi.fn(), approve: vi.fn(), reject: vi.fn(), success: vi.fn(), error: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: '11' }, query: {}, path: '/graphs/11/review' }), useRouter: () => ({ resolve: () => ({ fullPath: '/graphs/11/review' }) }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', () => ({ resolveCanonicalTabRouteState: () => ({ tab: 'entity', query: {}, changed: false }) }))
vi.mock('../src/composables/useKnowledgeGraphPageDescriptor', () => ({ useKnowledgeGraphPageDescriptor: vi.fn() }))
vi.mock('../src/utils/moduleNavigation', () => ({ navigateGraphRoute: vi.fn() }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => ({ hasPermission: () => true, hasAnyPermission: () => true }) }))
vi.mock('../src/api/graphBuild', () => ({ buildAPI: { listReviewItems: state.list, batchApprove: state.approve, batchReject: state.reject } }))
vi.mock('element-plus', async importOriginal => ({ ...(await importOriginal()), ElMessage: { success: state.success, error: state.error } }))

beforeEach(() => { vi.clearAllMocks() })
for (const action of ['approve', 'reject']) {
  for (const fails of [true, false]) {
    it(`refreshes actual results and clears stale selection after ${action} ${fails ? 'failure' : 'success'}`, async () => {
      state.list.mockResolvedValueOnce({ data: [{ id: 1 }, { id: 2 }], total: 2 }).mockResolvedValue({ data: [{ id: 2 }], total: 1 })
      state[action].mockImplementation(() => fails ? Promise.reject({ response: { data: { error: 'Some review items failed; successful results preserved.' } } }) : Promise.resolve({ message: 'ok' }))
      const wrapper = shallowMount(ReviewQueue, { global: { renderStubDefaultSlot: true, directives: { loading: () => {} }, stubs: { ElTableColumn: { template: '<div />' }, ElDialog: { template: '<div />' }, ElTable: { name: 'ElTable', props: ['data'], template: '<div><slot /></div>' } } } })
      try {
        await flushPromises()
        wrapper.findComponent({ name: 'ElTable' }).vm.$emit('selection-change', [{ id: 1 }, { id: 2 }])
        await flushPromises()
        const button = wrapper.findAllComponents({ name: 'ElButton' }).find(item => item.text().includes(action === 'approve' ? 'graph.review.batchApprove' : 'graph.review.batchReject'))
        await button.trigger('click'); await flushPromises()
        expect(state[action]).toHaveBeenCalledWith('11', [1, 2])
        expect(state.list).toHaveBeenCalledTimes(2)
        expect(wrapper.findComponent({ name: 'ElTable' }).props('data')).toEqual([{ id: 2 }])
        expect(wrapper.findAllComponents({ name: 'ElButton' }).some(item => item.text().includes('graph.review.batchApprove'))).toBe(false)
        if (fails) {
          expect(state.success).not.toHaveBeenCalled()
          expect(state.error).toHaveBeenCalledWith('Some review items failed; successful results preserved.')
        } else {
          expect(state.success).toHaveBeenCalledOnce()
          expect(state.error).not.toHaveBeenCalled()
        }
      } finally { wrapper.unmount() }
    })
  }
}
