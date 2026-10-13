import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import A2UISurface from '../src/a2ui/A2UISurface.vue'
import Snapshot from '../src/a2ui/components/TransferDraftSnapshot.vue'
import Table from '../../basic/src/components/TabularResultRenderer.vue'
import JsonPreview from '../../basic/src/components/previews/JsonPreview.vue'
import zh from '../../../agent/frontend/src/i18n/zh-cn.json'
import en from '../../../agent/frontend/src/i18n/en.json'

const reference = `draft:cccccccc-cccc-cccc-cccc-cccccccccccc:${'a'.repeat(64)}`
const parameters = () => ({ name: 'readonly_persons', config: {
  source: { locator: 'addp://engine/11/path/Outdoor/Persons?type=collection&item_id=106', data_type: 'table', representation: 'native' },
  target: { parent_locator: 'addp://engine/2/path/outdoor?type=schema&node_id=3', name: 'persons_check',
    data_type: 'table', representation: 'native', policy: { apply_mode: 'replace' } },
  runtime: { boundary: 'bounded' }, load: { mode: 'snapshot' }
} })
const snapshot = () => ({ reference, name: 'readonly_persons', parameters: JSON.stringify(parameters(), null, 2),
  mappings: [{ source: 'members', target: 'members', target_type: 'json', nullable: null, precision: null, scale: null }] })
const operations = value => [
  { version: 'v0.9', createSurface: { surfaceId: 'surface:draft', catalogId: 'addp.catalog/v1' } },
  { version: 'v0.9', updateComponents: { surfaceId: 'surface:draft', components: [{ id: 'root', component: 'TransferDraftSnapshot', ...value }] } }
]
const locale = () => createI18n({ legacy: false, locale: 'zh-cn', messages: { 'zh-cn': zh, en } })
const render = (value, language = locale()) => mount(A2UISurface, {
  props: { operations: operations(value) }, global: { plugins: [ElementPlus, language], stubs: { transition: false } }
})

describe('exact readonly checked draft snapshot', () => {
  it('reuses the shared table and JSON view, preserves every mapping and switches language', async () => {
    const value = snapshot()
    value.mappings = Array.from({ length: 128 }, (_, index) => ({ ...value.mappings[0], source: `members_${index}` }))
    value.mappings[0].source = '<img src=x onerror=alert(1)>'
    const language = locale()
    const wrapper = render(value, language)
    await vi.waitFor(() => expect(wrapper.findComponent(Snapshot).exists()).toBe(true))
    expect(wrapper.findComponent(Table).props('rows')).toEqual(value.mappings)
    expect(wrapper.findComponent(JsonPreview).props('data').object.content.text).toBe(value.parameters)
    expect(wrapper.get('pre').text()).toBe(value.parameters)
    expect(wrapper.text()).toContain(reference)
    expect(wrapper.text()).toContain('精确字段映射 (128)')
    expect(wrapper.get('[data-testid="draft-sourceLocation"]').text()).toBe('引擎 11 · Outdoor.Persons')
    expect(wrapper.get('[data-testid="draft-targetLocation"]').text()).toBe('引擎 2 · outdoor.persons_check')
    expect(wrapper.get('[data-testid="draft-boundary"]').text()).toBe('bounded')
    expect(wrapper.get('[data-testid="draft-loadMode"]').text()).toBe('snapshot')
    expect(wrapper.get('[data-testid="draft-applyPolicy"]').text()).toBe('replace · 运行时先清理再写入')
    expect(wrapper.get('[data-testid="draft-mappingCount"]').text()).toBe('128')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.findAll('details').every(detail => detail.attributes('open') === undefined)).toBe(true)
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.emitted('action')).toBeUndefined()
    language.global.locale.value = 'en'
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain(en.agent.chat.presentation.draft.title)
    expect(wrapper.text()).toContain(en.agent.chat.presentation.draft.notice)
    expect(wrapper.get('[data-testid="draft-sourceLocation"]').text()).toBe('Engine 11 · Outdoor.Persons')
    expect(wrapper.get('[data-testid="draft-applyPolicy"]').text()).toBe(en.agent.chat.presentation.draft.replace)
    wrapper.unmount()
  })

  it('formats complete locator paths with the shared formatter and escapes names without inferring data facts', async () => {
    const input = parameters()
    input.config.source.locator = 'addp://engine/15/path/%E5%8C%97%E4%BA%AC/a%2Fb?type=collection'
    input.config.target.parent_locator = 'addp://engine/18/path/private_schema?type=schema'
    input.config.target.name = '<img src=x onerror=alert(1)>'
    input.config.target.policy.apply_mode = 'append'
    const value = { ...snapshot(), parameters: JSON.stringify(input, null, 2) }
    const wrapper = render(value)
    await vi.waitFor(() => expect(wrapper.findComponent(Snapshot).exists()).toBe(true))
    expect(wrapper.get('[data-testid="draft-sourceLocation"]').text()).toBe('引擎 15 · 北京.a/b')
    expect(wrapper.get('[data-testid="draft-targetLocation"]').text()).toBe(`引擎 18 · private_schema.${input.config.target.name}`)
    expect(wrapper.get('[data-testid="draft-applyPolicy"]').text()).toBe(zh.agent.chat.presentation.draft.append)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.get('pre').text()).toBe(value.parameters)
    expect(wrapper.get('.draft-overview').text()).not.toMatch(/JSONB|一文档一行|已创建|已授权/)
    wrapper.unmount()
  })

  it('does not invent missing facts or reinterpret unknown mode literals', async () => {
    const value = { ...snapshot(), parameters: JSON.stringify({ config: { runtime: { boundary: 'future-mode' } } }) }
    const wrapper = render(value)
    await vi.waitFor(() => expect(wrapper.findComponent(Snapshot).exists()).toBe(true))
    for (const key of ['sourceLocation', 'targetLocation', 'loadMode', 'applyPolicy']) {
      expect(wrapper.get(`[data-testid="draft-${key}"]`).text()).toBe(zh.agent.chat.presentation.draft.unspecified)
    }
    expect(wrapper.get('[data-testid="draft-boundary"]').text()).toBe('future-mode')
    expect(wrapper.get('pre').text()).toBe(value.parameters)
    expect(wrapper.emitted('action')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([
    value => ({ ...value, action: 'create' }),
    value => ({ ...value, url: 'https://untrusted.example' }),
    value => ({ ...value, principal_id: 'private' }),
    value => ({ ...value, reference: 'draft:invalid' }),
    value => ({ ...value, parameters: '[1]' }),
    value => ({ ...value, parameters: '{' }),
    value => ({ ...value, parameters: JSON.stringify({ large: '中'.repeat(45000) }) }),
    value => ({ ...value, mappings: Array(513).fill(value.mappings[0]) }),
    value => ({ ...value, mappings: Array(512).fill({ ...value.mappings[0], source: 'x'.repeat(600) }) }),
    value => ({ ...value, mappings: [{ ...value.mappings[0], action: 'run' }] })
  ])('rejects invalid, oversized and action-bearing projections %#', async mutate => {
    const wrapper = render(mutate(snapshot()))
    await vi.waitFor(() => expect(wrapper.emitted('error')).toHaveLength(1))
    expect(wrapper.findComponent(Snapshot).exists()).toBe(false)
    expect(wrapper.emitted('action')).toBeUndefined()
    wrapper.unmount()
  })
})
