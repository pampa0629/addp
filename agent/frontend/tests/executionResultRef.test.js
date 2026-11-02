import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import ExecutionResultRef from '../src/components/ExecutionResultRef.vue'
const { get, resolveUrl } = vi.hoisted(() => ({ get: vi.fn(), resolveUrl: vi.fn(route => `http://console.example${route}`) }))
vi.mock('../src/api/index', () => ({ resultRefAPI: { getDevelopExecution: get } }))
vi.mock('@common-ui', () => ({ resolveTaskOwnerUrl: resolveUrl }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
beforeEach(() => { vi.clearAllMocks(); get.mockResolvedValue({ status: 'success' }) })
it('opens the same Develop execution through the Console origin rather than the module iframe', async () => {
  const open = vi.spyOn(window, 'open').mockImplementation(() => null)
  const wrapper = mount(ExecutionResultRef, { props: { resultRef: { ref: 'execution:run/1' } }, global: { stubs: { 'el-icon': true, 'el-tag': true, 'el-tooltip': { template: '<div><slot /></div>' }, 'el-button': { template: '<button />' } } } })
  await flushPromises()
  await wrapper.find('button').trigger('click')
  expect(get).toHaveBeenCalledWith('run/1')
  expect(resolveUrl).toHaveBeenCalledWith('/develop/executions/run%2F1')
  expect(open).toHaveBeenCalledWith('http://console.example/develop/executions/run%2F1', '_blank', 'noopener,noreferrer')
  wrapper.unmount(); open.mockRestore()
})
