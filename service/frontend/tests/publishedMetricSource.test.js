import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import * as Vue from 'vue'
import { parse, compileScript } from '@vue/compiler-sfc'
import { runInNewContext } from 'node:vm'
import { createModelMetricAPI, publishedMetricSources } from '../../../common-frontend/basic/src/api/modelMetrics.js'

const source = readFileSync(new URL('../src/components/PublishedMetricSourcePicker.vue', import.meta.url), 'utf8')
const descriptor = parse(source).descriptor
let code = compileScript(descriptor, { id: 'metric-picker' }).content
code = code.replace(/import \{([^}]+)\} from 'vue'/g, (_, names) => `const {${names}} = Vue`)
  .replace(/^import .*$/gm, '').replace('export default', 'return')
const create = new Function('Vue', 'useI18n', 'openConsoleRoute', 'createModelMetricAPI', 'publishedMetricSources', 'client', code)
const choices = [{ id: 3, name: 'Count', revisions: [{ id: 4, revision_no: 2, status: 'published' }, { id: 5, revision_no: 3, status: 'draft' }] }]
function harness(get, initial = null) {
  let mounted, unmount
  const props = { modelValue: initial }
  const writes = []
  const component = create({ ...Vue, onMounted: f => { mounted = f }, onBeforeUnmount: f => { unmount = f } }, () => ({ t: key => key }), () => {}, createModelMetricAPI, publishedMetricSources, { get })
  const state = component.setup(props, { expose() {}, emit(_event, value) { writes.push(value); props.modelValue = value } })
  return { state, writes, mounted, unmount, props }
}
test('loading is lazy; explicit revision selection emits only an owner reference', async () => {
  let calls = 0
  const h = harness(async () => { calls++; return choices })
  assert.equal(calls, 0)
  await h.mounted()
  assert.equal(calls, 1)
  assert.equal(h.props.modelValue, null)
  h.state.implementationId.value = 3
  h.state.selectImplementation()
  assert.equal(h.props.modelValue, null)
  h.state.revisionId.value = 4
  h.state.selectRevision()
  assert.deepEqual(h.props.modelValue, { implementation_id: 3, revision_id: 4 })
  h.state.revisionId.value = 5
  h.state.selectRevision()
  assert.equal(h.props.modelValue, null, 'draft cannot be selected')
})
test('unavailability and permission failures invalidate stale selections and can be retried', async () => {
  for (const status of [403, 503]) {
    let fail = true
    const h = harness(async () => { if (fail) throw { response: { status } }; return choices }, { implementation_id: 3, revision_id: 4 })
    await h.mounted()
    assert.equal(h.props.modelValue, null)
    assert.equal(h.state.errorKey.value, status === 403 ? 'service.query.metricSourcesForbidden' : 'service.query.metricSourcesUnavailable')
    fail = false
    await h.state.loadSources()
    assert.equal(h.state.errorKey.value, '')
    assert.equal(h.props.modelValue, null, 'retry never selects latest automatically')
  }
})
test('returning to selection revalidates the exact revision; withdrawal clears it', async () => {
  const h = harness(async () => choices, { implementation_id: 3, revision_id: 4 })
  await h.mounted()
  assert.deepEqual(h.props.modelValue, { implementation_id: 3, revision_id: 4 })
  const removed = harness(async () => [], h.props.modelValue)
  await removed.mounted()
  assert.equal(removed.props.modelValue, null)
})
test('late results after retry or switching source cannot change the selected source', async () => {
  const pending = []
  const h = harness(() => new Promise(resolve => pending.push(resolve)))
  const first = h.mounted()
  const second = h.state.loadSources()
  pending[1]([])
  await second
  pending[0](choices)
  await first
  assert.deepEqual(h.state.sources.value, [])
  const third = h.state.loadSources()
  h.unmount()
  const count = h.writes.length
  pending[2](choices)
  await third
  assert.deepEqual(h.state.sources.value, [])
  assert.equal(h.writes.length, count)
})

const formSource = readFileSync(new URL('../src/views/QueryServiceForm.vue', import.meta.url), 'utf8')
const submitCode = formSource.slice(formSource.indexOf('const handleSubmit = async'), formSource.indexOf('// 方法：返回列表'))
function submission(type, fail = false) {
  const sent = [], messages = [], navigation = []
  const context = {
    form: { config_type: type, service_name: 'count', title: 'Count', engine_id: 2, runtime_engine_id: 9, schema_name: 'public', table_name: 'fact', locator: 'test-locator', sql_query: 'SELECT count(*) AS value FROM fact' },
    metricSource: { value: { implementation_id: 3, revision_id: 4 } },
    versionConflict: { value: false }, submitting: { value: false }, isEdit: { value: false },
    loading: { value: false }, editorVersion: 0, captureDraft: () => '', markSaved() {},
    tableUsesRuntime: { value: false }, defaultFieldsInput: { value: '' }, filterableFieldsInput: { value: '' },
    parseFieldInput: () => [], sqlNamedParameters: { value: [] }, sqlStableKey: { value: ['value'] },
    buildSQLOutputContract: () => ({ fields: [{ name: 'value' }] }), enableOgcFeatures: { value: true },
    queryServiceAPI: { createService: async payload => { sent.push(JSON.parse(JSON.stringify(payload))); if (fail) throw new Error('revision withdrawn'); return { id: 41 } } },
    ElMessage: Object.fromEntries(['success', 'warning', 'error'].map(kind => [kind, text => messages.push({ kind, text })])),
    t: key => key, console: { error() {} }, router: {},
    navigateServiceRoute: async (_router, path) => navigation.push(path)
  }
  const submit = runInNewContext(`${submitCode}; handleSubmit`, context)
  return { context, submit, sent, messages, navigation }
}
test('metric creation submits an exact reference without leaking table or SQL configuration', async () => {
  const h = submission('analytical')
  await h.submit()
  assert.deepEqual(h.sent[0].metric_source, { implementation_id: 3, revision_id: 4 })
  for (const key of ['engine_id', 'runtime_engine_id', 'sql_query', 'schema_name', 'table_name', 'named_parameters', 'data_config', 'output_contract']) assert.equal(key in h.sent[0], false, key)
  assert.deepEqual(h.sent[0].protocols, { rest_api: { enabled: true, formats: ['json'] } })
  assert.deepEqual(h.navigation, ['/query-services/41'])
})
test('creation failure keeps the chosen revision and form available for correction', async () => {
  const h = submission('analytical', true)
  await h.submit()
  assert.equal(h.context.metricSource.value.revision_id, 4)
  assert.equal(h.context.form.service_name, 'count')
  assert.equal(h.context.submitting.value, false)
  assert.equal(h.navigation.length, 0)
  assert.match(h.messages[0].text, /revision withdrawn/)
})
test('editing metric service metadata preserves its existing output protocols', async () => {
  const h = submission('analytical')
  h.context.isEdit.value = true
  h.context.form.version = 3
  h.context.route = { params: { id: '35' } }
  h.context.queryServiceAPI.updateService = async (_id, payload) => {
    h.sent.push(JSON.parse(JSON.stringify(payload)))
    return { version: 4 }
  }
  await h.submit()
  assert.equal(h.sent.length, 1)
  assert.equal('protocols' in h.sent[0], false, 'read-only protocol configuration must not be overwritten')
  assert.equal('metric_source' in h.sent[0], false)
  assert.equal(h.context.form.version, 4)
})
test('missing metric choice blocks only metric creation; ordinary sources need no Model client', async () => {
  for (const type of ['analytical', 'table', 'sql']) {
    const h = submission(type)
    h.context.metricSource.value = null
    await h.submit()
    assert.equal(h.sent.length, type === 'analytical' ? 0 : 1)
    if (type !== 'analytical') assert.equal('metric_source' in h.sent[0], false)
  }
  const root = parse(formSource).descriptor.template.content
  assert.match(root, /v-if="!isEdit && currentStep === 1"[\s\S]*v-if="form.config_type === 'analytical'"[^>]*>\s*<PublishedMetricSourcePicker/)
})

test('detail result selection is explicit and cleared for a revision without details', async () => {
  const items = [{ id: 3, name: 'Count', revisions: [
    { id: 4, revision_no: 2, status: 'published', contract: { include_details: true } },
    { id: 6, revision_no: 4, status: 'published', contract: {} }
  ] }]
  const h = harness(async () => items, { implementation_id: 3, revision_id: 4, result_kind: 'details' })
  await h.mounted()
  assert.deepEqual(h.props.modelValue, { implementation_id: 3, revision_id: 4, result_kind: 'details' })
  h.state.revisionId.value = 6
  h.state.selectRevision()
  assert.deepEqual(h.props.modelValue, { implementation_id: 3, revision_id: 6 })
  assert.equal(h.state.resultKind.value, '')
})
