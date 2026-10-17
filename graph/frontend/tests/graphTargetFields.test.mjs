// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import GraphTargetFields from '../src/components/GraphTargetFields.vue'
import { engineAPI } from '../src/api/ontology'
vi.mock('../src/api/ontology', () => ({ engineAPI: { getGraphEngines: vi.fn(), getGraphTargets: vi.fn() } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', () => ({ engineSelectionState: () => 'online', isEngineSelectable: engine => !!engine && engine.connection_status === 'online' }))
vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() }, ElSelect: { name: 'ElSelect', template: '<div><slot /></div>' }, ElOption: { name: 'ElOption', template: '<div><slot /></div>' }, ElFormItem: { name: 'ElFormItem', template: '<div><slot /></div>' } }))
const Select = defineComponent({ props: ['modelValue', 'disabled'], emits: ['update:model-value'], template: '<div><slot /></div>' })
const Shell = defineComponent({ template: '<div><slot /></div>' })
const engine = id => ({ id, name: `engine ${id}`, engine_type: id === 1 ? 'neo4j' : 'falkordb', connection_status: 'online' })
const graph = (id, name) => ({ name, path: { version: 'catalog.path/v1', engine_id: id, segments: [{ term: 'server', kind: 'server', name: '' }, { term: 'graph', kind: 'graph', name }] } })
function mountFields() {
  return mount(GraphTargetFields, { props: { modelValue: { engine_id: null, catalog_path: null } }, global: { stubs: { 'el-select': Select, 'el-option': Shell, 'el-form-item': Shell } } })
}
describe('graph target fields', () => {
  it('discards a delayed response for the previously selected engine', async () => {
    engineAPI.getGraphEngines.mockResolvedValue([engine(1), engine(2)])
    let finishFirst
    const current = graph(2, '中文.a/b')
    engineAPI.getGraphTargets.mockImplementation(id => id === 1 ? new Promise(resolve => { finishFirst = resolve }) : Promise.resolve([current]))
    const wrapper = mountFields(); await flushPromises()
    const selects = wrapper.findAllComponents(Select)
    selects[0].vm.$emit('update:model-value', 1); await flushPromises()
    await wrapper.setProps({ modelValue: { engine_id: 1, catalog_path: null } })
    selects[0].vm.$emit('update:model-value', 2); await flushPromises()
    await wrapper.setProps({ modelValue: { engine_id: 2, catalog_path: null } })
    finishFirst([graph(1, 'stale')]); await flushPromises()
    selects[1].vm.$emit('update:model-value', JSON.stringify(current.path)); await flushPromises()
    expect(wrapper.emitted('update:modelValue').at(-1)[0].catalog_path).toEqual(current.path)
    selects[1].vm.$emit('update:model-value', JSON.stringify(graph(1, 'stale').path)); await flushPromises()
    expect(wrapper.emitted('update:modelValue').at(-1)[0].catalog_path).toEqual(current.path)
    wrapper.unmount()
  })
})
