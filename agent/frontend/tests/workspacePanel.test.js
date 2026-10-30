import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const { runs } = vi.hoisted(() => ({ runs: vi.fn() }))
vi.mock('../src/api', () => ({ sessionAPI: { runs }, runAPI: {} }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
import WorkspacePanel from '../src/components/WorkspacePanel.vue'
const stubs = { 'el-tabs': { template: '<div><slot /></div>' }, 'el-tab-pane': { template: '<div><slot /></div>' },
  'el-empty': { props: ['description'], template: '<p>{{ description }}</p>' }, 'el-button': true, 'el-select': true, 'el-pagination': true }
describe('workspace run visibility', () => {
  beforeEach(() => runs.mockReset())
  it('finishes loading for a valid empty run page', async () => {
    runs.mockResolvedValue({ data: [], total: 0 })
    const wrapper = mount(WorkspacePanel, { props: { sessionId: '1', canReadRuns: true }, global: { stubs } })
    await flushPromises()
    expect(wrapper.text()).toContain('agent.workspace.noRuns')
    wrapper.unmount()
  })
  it('does not load run history without its independent permission', async () => {
    const wrapper = mount(WorkspacePanel, { props: { sessionId: '1', canReadRuns: false }, global: { stubs } })
    await flushPromises()
    expect(runs).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('agent.workspace.runReadRequired')
    wrapper.unmount()
  })
})
