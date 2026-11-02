import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../../basic/src/i18n/zh-cn.json'
import en from '../../basic/src/i18n/en.json'
import MarkdownContent from '../../basic/src/components/MarkdownContent.vue'

const feedback = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('element-plus', () => ({ ElMessage: feedback }))
const render = props => {
  const i18n = createI18n({ legacy: false, locale: 'zh-cn', messages: { 'zh-cn': zh, en } })
  return { wrapper: mount(MarkdownContent, { props, global: { plugins: [i18n] } }), i18n }
}

describe('shared Markdown body', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue() } })
  })
  it('renders and copies the latest partial fence while the body grows, then localizes without duplicating controls', async () => {
    const { wrapper, i18n } = render({ text: '开头🙂\n\n```sql\nSELECT', copyCode: true })
    expect(wrapper.find('code').text()).toBe('SELECT')
    await wrapper.setProps({ text: '开头🙂\n\n```sql\nSELECT \'<tag>\';\n```\n\n完成' })
    await wrapper.find('button').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenLastCalledWith("SELECT '<tag>';")
    expect(wrapper.text()).toContain('完成')
    i18n.global.locale.value = 'en'
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.find('button').text()).toBe('Copy code')
    await wrapper.setProps({ text: '' })
    expect(wrapper.findAll('button')).toHaveLength(0)
    wrapper.unmount()
  })
  it('sanitizes active content and never treats source-provided buttons as copy controls', async () => {
    const { wrapper } = render({ text: '<script>bad()</script><img src=x onerror="bad()"><a href="javascript:bad()">link</a><svg onload="bad()"></svg><button data-agent-copy-code>fake</button>\n\n```html\n<script>literal</script>\n```', copyCode: true })
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('svg').exists()).toBe(false)
    expect(wrapper.find('img').attributes('onerror')).toBeUndefined()
    expect(wrapper.find('a').attributes('href')).toBeUndefined()
    await wrapper.findAll('button').find(button => button.text() === 'fake').trigger('click')
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(button => button.text() === '复制代码').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('<script>literal</script>')
    wrapper.unmount()
  })
  it('reports clipboard denial and keeps the text available', async () => {
    navigator.clipboard.writeText.mockRejectedValue(new Error('denied'))
    const { wrapper } = render({ text: '```\nSELECT 1\n```', copyCode: true })
    await wrapper.find('button').trigger('click')
    expect(feedback.error).toHaveBeenCalledWith(zh.common.markdown.copyFailed)
    expect(wrapper.find('code').text()).toBe('SELECT 1')
    wrapper.unmount()
  })
  it('isolates line-break settings and leaves Mermaid as inert code in chat', () => {
    const first = render({ text: 'a\nb', breaks: true })
    const second = render({ text: 'a\nb\n\n```mermaid\ngraph LR\nA-->B\n```' })
    expect(first.wrapper.find('br').exists()).toBe(true)
    expect(second.wrapper.find('br').exists()).toBe(false)
    expect(second.wrapper.find('code.language-mermaid').text()).toContain('A-->B')
    expect(second.wrapper.find('svg').exists()).toBe(false)
    first.wrapper.unmount(); second.wrapper.unmount()
  })
})
