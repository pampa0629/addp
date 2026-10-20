// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { ElTable, ElPagination } from 'element-plus'
import { createI18n } from 'vue-i18n'
import { expect, it } from 'vitest'
import App from '../src/App.vue'

it('updates Element Plus table and pagination when the Graph language changes', async () => {
  const i18n = createI18n({ legacy: false, locale: 'zh-cn', messages: { 'zh-cn': {}, en: {} } })
  const wrapper = mount(App, {
    global: {
      plugins: [i18n],
      components: { RouterView: {
        components: { ElTable, ElPagination },
        template: '<section><el-table :data="[]" /><el-pagination :total="0" layout="total, prev, pager, next" /></section>',
      } },
    },
  })
  try {
    await flushPromises()
    expect(wrapper.text()).toContain('暂无数据')
    expect(wrapper.text()).toContain('共 0 条')
    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(wrapper.text()).toContain('No Data')
    expect(wrapper.text()).toContain('Total 0')
    i18n.global.locale.value = 'zh-cn'
    await flushPromises()
    expect(wrapper.text()).toContain('暂无数据')
  } finally { wrapper.unmount() }
})
