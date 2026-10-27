// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, createApp, h, inject, nextTick, provide, reactive } from 'vue'
import GovernanceTasks from '../src/views/GovernanceTasks.vue'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

const api = vi.hoisted(() => ({ route: null, i18n: null, response: null, listGovernanceTasks: vi.fn(), listEntries: vi.fn(), navigate: vi.fn(), error: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => api.route, useRouter: () => ({}) }))
vi.mock('vue-i18n', () => ({ useI18n: () => api.i18n }))
vi.mock('../src/api/catalog', () => api)
vi.mock('../src/store/auth', () => ({ useAuthStore: () => ({ hasPermission: () => true }) }))
vi.mock('@common-ui', () => ({ navigateConsoleModuleRoute: (...args) => api.navigate(...args) }))
vi.mock('element-plus', () => ({ ElMessage: { error: (...args) => api.error(...args) } }))

const mounted = []
const settle = async () => { for (let i = 0; i < 8; i++) { await Promise.resolve(); await nextTick() } }
const task = (id, resolution) => ({
  id, catalog_entry_id: `entry-${id}`, entry_display_name: `Resource ${id}`, responsibility_role: 'data_steward',
  observed_snapshot: { name: `Former steward ${id}` }, reason: 'subject_not_found', status: 'resolved', resolution,
  opened_at: '2026-01-01T08:00:00Z', resolved_at: '2026-01-02T09:00:00Z'
})
function mount() {
  const root = document.createElement('div'); document.body.appendChild(root)
  const app = createApp(GovernanceTasks)
  app.directive('loading', {})
  app.component('el-table', { props: ['data'], setup(props, { slots }) {
    provide('tableRows', computed(() => props.data))
    return () => h('div', slots.default?.())
  } })
  app.component('el-table-column', { props: ['label', 'prop'], setup(props, { slots }) {
    const rows = inject('tableRows')
    return () => h('section', { 'data-column': props.label }, rows.value.map(row => h('div', { 'data-row': row.id },
      slots.default ? slots.default({ row }) : row[props.prop])))
  } })
  for (const name of ['button', 'alert', 'card', 'form', 'form-item', 'select', 'option', 'tag', 'empty', 'pagination']) {
    app.component(`el-${name}`, { setup: (_, { slots }) => () => h('div', slots.default?.()) })
  }
  app.mount(root); mounted.push({ app, root })
  return root
}
beforeEach(async () => {
  vi.resetAllMocks()
  const { createI18n } = await vi.importActual('vue-i18n')
  api.i18n = createI18n({ legacy: false, locale: 'zh-cn', fallbackLocale: false, messages: { 'zh-cn': zh, en } }).global
  api.route = reactive({ query: { status: 'resolved' } })
  api.response = { data: [task('restored', 'reference_restored'), task('replaced', 'responsibility_replaced')], total: 2 }
  api.listGovernanceTasks.mockImplementation(async () => api.response)
})
afterEach(() => mounted.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() }))

describe('Catalog responsibility governance task presentation', () => {
  it('distinguishes restoration from replacement and shows both dates in Chinese and English', async () => {
    const root = mount(); await settle()
    const evidence = JSON.stringify(api.response)
    expect([...root.querySelectorAll('.task-resolution')].map(node => node.textContent)).toEqual(['主体引用已恢复', '原责任关系已移除或替换'])
    const zhTimes = root.querySelector('[data-column="发生/解决时间"]').textContent
    expect(zhTimes).toContain('发生时间')
    expect(zhTimes).toContain('解决时间')
    expect(zhTimes).toContain('2026年1月1日')
    expect(zhTimes).toContain('2026年1月2日')
    expect(root.textContent).toContain('Former steward restored')
    const zhText = root.textContent
    api.i18n.locale.value = 'en'; await settle()
    expect([...root.querySelectorAll('.task-resolution')].map(node => node.textContent)).toEqual(['Subject reference restored', 'Previous responsibility removed or replaced'])
    const enTimes = root.querySelector('[data-column="Opened/resolved"]').textContent
    expect(enTimes).toContain('Opened')
    expect(enTimes).toContain('Resolved')
    expect(enTimes).toContain('Jan 1, 2026')
    expect(enTimes).toContain('Jan 2, 2026')
    api.i18n.locale.value = 'zh-cn'; await settle()
    expect(root.textContent).toBe(zhText)
    expect(JSON.stringify(api.response)).toBe(evidence)
    expect(api.listGovernanceTasks).toHaveBeenCalledTimes(1)
    expect(api.listEntries).not.toHaveBeenCalled()
    expect(api.navigate).not.toHaveBeenCalled()
  })

  it('keeps an open task pending and does not show an unrecorded resolution date', async () => {
    api.route.query = {}
    api.response.data = [{ ...task('open', 'reference_restored'), status: 'open', resolved_at: null }]
    const root = mount(); await settle()
    expect(root.textContent).toContain('待处理')
    expect(root.querySelector('.task-resolution')).toBeNull()
    expect(root.querySelector('[data-column="发生/解决时间"]').textContent).toContain('发生时间')
    expect(root.querySelector('[data-column="发生/解决时间"]').textContent).not.toContain('解决时间')
  })

  it('marks missing or unknown resolution values without exposing technical codes', async () => {
    api.response.data = [task('missing', null), task('unknown', 'future_resolution')]
    const root = mount(); await settle()
    expect([...root.querySelectorAll('.task-resolution')].map(node => node.textContent)).toEqual(['解决方式未记录或无法识别', '解决方式未记录或无法识别'])
    api.i18n.locale.value = 'en'; await settle()
    expect([...root.querySelectorAll('.task-resolution')].map(node => node.textContent)).toEqual(['Resolution not recorded or unrecognized', 'Resolution not recorded or unrecognized'])
    expect(root.textContent).not.toContain('future_resolution')
    expect(root.textContent).not.toContain('[object Object]')
  })
})
