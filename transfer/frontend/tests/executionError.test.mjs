import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import * as Vue from 'vue'
import { renderToString } from '@vue/server-renderer'
import { parse } from '@vue/compiler-sfc'
import { compile } from '@vue/compiler-dom'

const source = readFileSync(new URL('../src/views/ExecutionDetail.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const { code } = compile(descriptor.template.content, { mode: 'function', prefixIdentifiers: true })
const render = new Function('Vue', code)(Vue)

// Render the actual page template with API-shaped data. No live task or API is invoked.
async function renderExecution(execution, language) {
  const messages = JSON.parse(readFileSync(new URL(`../src/i18n/${language}.json`, import.meta.url), 'utf8'))
  const app = Vue.createSSRApp({
    render,
    setup: () => ({
      execution, loading: false, isContinuousExecution: false,
      authStore: { hasPermission: () => false },
      executionAPI: { events: () => {} },
      getStatusType: () => 'danger',
      t: key => key.split('.').reduce((value, part) => value?.[part], messages)
    })
  })
  const container = { setup: (_, { slots }) => () => Vue.h('div', slots.default?.()) }
  for (const name of ['ElCard', 'ElDescriptions', 'ElDescriptionsItem', 'ElTag']) app.component(name, container)
  app.component('ExecutionEvents', { render: () => null })
  app.component('MonitorExecutionsButton', { render: () => null })
  app.directive('loading', {})
  return renderToString(app)
}

test('execution detail renders backend error_msg as escaped text in both languages', async () => {
  const error = 'sql: converting argument $3 type: unsupported type []interface {}\n<script>alert(1)</script>'
  for (const language of ['zh-cn', 'en']) {
    const html = await renderExecution({ execution_id: 'fixture', status: 'failed', error_msg: error }, language)
    assert.match(html, /data-testid="execution-error-message"/)
    assert.match(html, /unsupported type \[\]interface \{\}\n&lt;script&gt;alert\(1\)&lt;\/script&gt;/)
    assert.doesNotMatch(html, /<script>/)
  }
})

test('execution detail omits the error section when no backend error is present', async () => {
  for (const execution of [{ status: 'running' }, { status: 'success', error_msg: '' }]) {
    assert.doesNotMatch(await renderExecution(execution, 'zh-cn'), /data-testid="execution-error-message"/)
  }
})
