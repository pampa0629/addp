// @vitest-environment jsdom
import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import GraphBrowser from '../src/views/GraphBrowser.vue'
import { browseAPI } from '../src/api/browse'

vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: '2' } }), useRouter: () => ({}) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', () => ({ useConsolePageDescriptor: vi.fn() }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => ({ hasPermission: () => false }) }))
vi.mock('../src/api/browse', () => ({ browseAPI: { getBrowseSnapshot: vi.fn(), searchNodes: vi.fn() } }))
vi.mock('../src/api/ontology', () => ({ knowledgeGraphAPI: { get: vi.fn(async () => ({ name: 'Protected graph' })) } }))
vi.mock('../src/api/analysis', () => ({ analysisAPI: {} }))
vi.mock('../src/components/GraphCanvas.vue', () => ({ default: {
  name: 'GraphCanvas', props: ['nodes', 'edges'], template: '<div data-testid="canvas">{{ nodes }}</div>',
  methods: { resetNodeColors() {}, focusNodes() {} },
} }))
vi.mock('../src/components/NodePanel.vue', () => ({ default: { name: 'NodePanel', template: '<div />' } }))
vi.mock('../src/components/AnalysisPanel.vue', () => ({ default: { name: 'AnalysisPanel', template: '<div />' } }))
vi.mock('@addp/common-frontend/graph', () => ({
  getContrastingTextColor: () => '#fff', readGraphTheme: () => ({ categoryColors: ['#123456'] }),
}))

let wrapper
const snapshot = name => ({
  schema: { node_shapes: [], relationship_shapes: [] },
  stats: { node_count: 93, relationship_count: 132 },
  overview: { nodes: [{ id: name, name, labels: [] }], edges: [] },
})
const denied = () => Object.assign(new Error('Request failed with status code 403'), {
  response: { status: 403, data: { error: 'Reading is denied by data protection' } },
})

function mountBrowser() {
  return shallowMount(GraphBrowser, { global: { stubs: {
    GraphCanvas: false,
    ElAlert: { props: ['title', 'description'], template: '<div role="alert">{{ title }} {{ description }}</div>' },
    ElButton: { template: '<button><slot /></button>' },
    ElTag: { template: '<span><slot /></span>' },
    ElInput: { props: ['modelValue'], emits: ['update:modelValue', 'keyup'], template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" @keyup="$emit(\'keyup\', $event)" />' },
  } } })
}

beforeEach(() => { vi.clearAllMocks() })
afterEach(() => { wrapper?.unmount() })

it('keeps a denied initial browse visible as an error instead of an empty graph', async () => {
  browseAPI.getBrowseSnapshot.mockRejectedValueOnce(denied())
  wrapper = mountBrowser()
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('Reading is denied by data protection')
  expect(wrapper.find('[data-testid="canvas"]').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('graph.browser.overviewCount')
})

it('removes previously displayed data after a denied operation and recovers only after a successful retry', async () => {
  browseAPI.getBrowseSnapshot.mockResolvedValueOnce(snapshot('previous-node'))
  wrapper = mountBrowser()
  await flushPromises()
  expect(wrapper.get('[data-testid="canvas"]').text()).toContain('previous-node')
  browseAPI.searchNodes.mockRejectedValueOnce(denied())
  await wrapper.get('input').setValue('task')
  await wrapper.get('input').trigger('keyup', { key: 'Enter' })
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('Reading is denied by data protection')
  expect(wrapper.text()).not.toContain('previous-node')
  expect(wrapper.text()).not.toContain('93')

  browseAPI.getBrowseSnapshot.mockResolvedValueOnce(snapshot('current-node'))
  await wrapper.get('button[title="graph.browser.resetView"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.get('[data-testid="canvas"]').text()).toContain('current-node')
  expect(wrapper.text()).not.toContain('previous-node')
})
