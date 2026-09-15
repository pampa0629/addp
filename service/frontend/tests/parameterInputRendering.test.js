import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import * as Vue from 'vue'
import { createI18n, useI18n } from 'vue-i18n'
import { parse, compileScript } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'

// Compile the actual shared component. The simple select/option primitives keep
// this test focused on the published values, localization and disabled state.
test('published option labels switch language while machine values stay unchanged', async () => {
  const source = readFileSync(new URL('../../../common-frontend/basic/src/components/ParameterValueInput.vue', import.meta.url), 'utf8')
  const { descriptor } = parse(source)
  let code = compileScript(descriptor, { id: 'parameter-options', inlineTemplate: true }).content
  code = code.replace(/import \{([^}]+)\} from ["']vue["']/g, (_, names) => `const {${names.replace(/ as /g, ': ')}} = Vue`)
    .replace(/import \{ useI18n \} from 'vue-i18n'/, '')
    .replace('export default', 'return')
  const Component = new Function('Vue', 'useI18n', code)(Vue, useI18n)
  const options = [{ value: 'total', labels: { 'zh-cn': '全期', en: 'Total' } }, { value: 'month', labels: { 'zh-cn': '按月', en: 'Monthly' } }]
  const i18n = createI18n({ legacy: false, locale: 'zh-cn', messages: {} })
  const app = Vue.createSSRApp(Component, { modelValue: 'month', controlType: 'text', options, disabled: true })
  app.use(i18n)
  app.component('el-select', { props: ['disabled'], setup: (props, { slots }) => () => Vue.h('select', { disabled: props.disabled }, slots.default?.()) })
  app.component('el-option', { props: ['value','label'], setup: props => () => Vue.h('option', { value: props.value }, props.label) })
  const chinese = await renderToString(app)
  assert.match(chinese, /全期/)
  assert.match(chinese, /按月/)
  assert.match(chinese, /value="month"/)
  assert.match(chinese, /disabled/)
  i18n.global.locale.value = 'en'
  const english = await renderToString(app)
  assert.match(english, /Monthly/)
  assert.doesNotMatch(english, /按月/)
  assert.match(english, /value="month"/)
})
