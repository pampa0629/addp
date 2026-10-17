import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import * as Vue from 'vue'
import { renderToString } from '@vue/server-renderer'
import { parse } from '@vue/compiler-sfc'
import { compile } from '@vue/compiler-dom'
import { executionFailurePresentation } from '../../../common-frontend/basic/src/utils/executionFailure.mjs'

const source = readFileSync(new URL('../src/views/ExecutionDetail.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const { code } = compile(descriptor.template.content, { mode: 'function', prefixIdentifiers: true })
const render = new Function('Vue', code)(Vue)

// Render the actual page template with API-shaped data. No live task or API is invoked.
async function renderExecution(execution, language) {
  const failureProps = []
  const commonMessages = JSON.parse(readFileSync(new URL(`../../../common-frontend/basic/src/i18n/${language}.json`, import.meta.url), 'utf8'))
  const messages = JSON.parse(readFileSync(new URL(`../src/i18n/${language}.json`, import.meta.url), 'utf8'))
  const app = Vue.createSSRApp({
    render,
    setup: () => ({
      execution, loading: false, isContinuousExecution: false,
      authStore: { hasPermission: () => false },
      executionAPI: { events: () => {} },
      getStatusType: () => 'danger',
      t: key => key.split('.').reduce((value, part) => value?.[part], key.startsWith('common.') ? commonMessages : messages)
    })
  })
  const container = { setup: (_, { slots }) => () => Vue.h('div', slots.default?.()) }
  for (const name of ['ElCard', 'ElDescriptions', 'ElDescriptionsItem', 'ElTag']) app.component(name, container)
  app.component('ExecutionEvents', { render: () => null })
  app.component('ExecutionFailure', { props: ['status', 'errorDetails', 'executionId', 'showMonitorLink'], setup(props) { failureProps.push({ ...props }); return () => null } })
  app.component('MonitorExecutionsButton', { render: () => null })
  app.directive('loading', {})
  return { html: await renderToString(app), failureProps }
}

test('execution detail delegates the exact safe API contract to the sole shared failure panel', async () => {
  for (const language of ['zh-cn', 'en']) {
    const details = { category: 'execution_failed', code: 'data_protection_required', diagnostic: {
      schema_version: 'addp.execution-failure/v3', stage: 'preparation', reason: 'data_protection_required'
    } }
    const { html, failureProps } = await renderExecution({ execution_id: 'fixture', status: 'failed', error_details: details,
      error_msg: 'private-sentinel<script>alert(1)</script>' }, language)
    assert.equal(failureProps.length, 1)
    assert.deepEqual(failureProps[0], { status: 'failed', executionId: 'fixture', errorDetails: details, showMonitorLink: false })
    assert.doesNotMatch(html, /private-sentinel|<script>/)
    assert.equal((descriptor.template.content.match(/<ExecutionFailure\b/g) || []).length, 1)
    assert.doesNotMatch(descriptor.template.content, /execution-error-message|common\.executionFailure\./)
  }
})

test('execution detail leaves non-failed visibility and absent diagnostics to the shared component', async () => {
  for (const execution of [{ status: 'running' }, { status: 'success', error_details: {} }]) {
    const { failureProps } = await renderExecution(execution, 'zh-cn')
    assert.equal(failureProps[0].status, execution.status)
    assert.deepEqual(failureProps[0].errorDetails, {})
  }
})

test('professional diagnostics have no worker identity, lease or fencing controls', () => {
  for (const field of ['owner_instance_id', 'fencing_token', 'heartbeat_at', 'lease_until']) {
    assert.ok(!descriptor.template.content.includes(field), `private runtime field remains: ${field}`)
  }
  for (const language of ['zh-cn', 'en']) {
    const messages = JSON.parse(readFileSync(new URL(`../src/i18n/${language}.json`, import.meta.url), 'utf8'))
    for (const key of ['workerInstance', 'fencingToken', 'heartbeatAt', 'leaseUntil']) {
      assert.equal(messages.transfer.executionDetail[key], undefined)
    }
  }
})

test('real shared failure template renders localized write position and target warning', async () => {
  const component = parse(readFileSync(new URL('../../../common-frontend/basic/src/components/ExecutionFailure.vue', import.meta.url), 'utf8')).descriptor
  const compiled = compile(component.template.content, { mode: 'function', prefixIdentifiers: true })
  const sharedRender = new Function('Vue', compiled.code)(Vue)
  for (const language of ['zh-cn', 'en']) {
    const messages = JSON.parse(readFileSync(new URL(`../../../common-frontend/basic/src/i18n/${language}.json`, import.meta.url), 'utf8'))
    const t = (key, params = {}) => key.split('.').reduce((value, part) => value?.[part], messages)?.replace(/\{(\w+)\}/g, (_, key) => params[key])
    const details = { diagnostic: { schema_version: 'addp.execution-failure/v3', stage: 'execution', reason: 'json_invalid',
      write: { column_index: 3, declared_type: 'json' }, target_change_possible: true }, message: 'private-value SELECT password' }
    const presentation = executionFailurePresentation(details, t)
    const app = Vue.createSSRApp({ render: sharedRender, setup: () => ({ t, presentation, failed: true,
      fallbackMessage: '', executionId: 'fixture-execution', showMonitorLink: false, ownerRoute: '', copyDetails: () => {} }) })
    app.component('ElButton', { setup: (_, { slots }) => () => Vue.h('button', slots.default?.()) })
    const html = await renderToString(app)
    for (const expected of [presentation.reason, presentation.write, presentation.targetWarning, 'fixture-execution']) assert.ok(html.includes(expected), expected)
    assert.doesNotMatch(html, /private-value|SELECT|password/)
  }
})
