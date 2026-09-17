import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import test from 'node:test'
import { ref, computed, watch, effectScope } from 'vue'
import { createI18n } from 'vue-i18n'

const source = await readFile(new URL('../src/views/QueryServiceDetail.vue', import.meta.url), 'utf8')
const actions = source.slice(source.indexOf('const captureVersionConflict ='), source.indexOf('// 方法：删除服务'))
function harness(updateService) {
  const state = {
    service: { value: { id: 35, version: 3, status: 'inactive', public_access: false } },
    serviceId: { value: 35 }, statusUpdating: { value: false }, versionConflict: { value: false },
    metricBindingRequired: { value: false },
    previewNamedParameterValues: { subject_id: 'person', grain: 'month' },
    previewData: { value: [{ value: 5 }] },
    previewPagination: { value: { page: 2, pageSize: 20, hasMore: true, nextCursor: 'old', cursors: ['', 'old'] } },
    queryServiceAPI: { updateService }, ElMessage: { success() {}, error() {}, warning() {} }, t: key => key
  }
  return { state, toggle: vm.runInContext(`${actions}\ntoggleServiceStatus`, vm.createContext(state)) }
}

test('activation and deactivation use the returned version and preserve query parameters and access', async () => {
  const requests = []
  const { state, toggle } = harness(async (id, body) => {
    requests.push({ id, body: JSON.parse(JSON.stringify(body)) })
    return { id, ...body, version: body.version + 1, public_access: false }
  })
  await toggle()
  assert.equal(state.service.value.status, 'active')
  assert.equal(state.service.value.version, 4)
  assert.equal(state.service.value.public_access, false)
  assert.equal(state.previewNamedParameterValues.subject_id, 'person')
  assert.equal(state.previewData.value.length, 0)
  assert.equal(state.previewPagination.value.page, 1)
  assert.equal(state.previewPagination.value.nextCursor, '')
  await toggle()
  assert.deepEqual(requests, [
    { id: 35, body: { version: 3, status: 'active' } },
    { id: 35, body: { version: 4, status: 'inactive' } }
  ])
})

test('conflict keeps local state and inputs and never retries a stale activation', async () => {
  let calls = 0
  const { state, toggle } = harness(async () => {
    calls++
    throw { response: { data: { error_code: 'resource_version_conflict' } } }
  })
  await toggle()
  assert.equal(state.versionConflict.value, true)
  assert.equal(state.service.value.status, 'inactive')
  assert.equal(state.service.value.version, 3)
  assert.equal(state.previewNamedParameterValues.subject_id, 'person')
  assert.equal(state.statusUpdating.value, false)
  await toggle()
  assert.equal(calls, 1)
})

test('incomplete metric cannot be activated and inactive preview makes no query', async () => {
  const { state, toggle } = harness(async () => assert.fail('unbound metric activated'))
  state.metricBindingRequired.value = true
  await toggle()
  state.queryUnavailable = { value: true }
  state.queryServiceAPI.testQuery = () => assert.fail('inactive query was sent')
  const preview = source.slice(source.indexOf('const loadPreviewData ='), source.indexOf('const handlePreviewPageChange ='))
  await vm.runInContext(`${preview}\nloadPreviewData`, vm.createContext(state))()
  assert.match(source, /:disabled="queryUnavailable"/)
  assert.match(source, /:disabled="queryUnavailable \|\| !isProtocolEnabled/)
})

test('engine display uses live names and distinguishes loading and unavailable metadata', () => {
  const displaySource = source.slice(source.indexOf('const engineDisplay ='), source.indexOf('onMounted(async () =>'))
  const state = { engines: { value: [{ id: 2, name: 'Business PostgreSQL', engine_type: 'postgresql' }] }, enginesLoading: { value: false }, enginesUnavailable: { value: false }, t: (key, values) => `${key}:${values?.id || ''}` }
  const display = vm.runInContext(`${displaySource}\nengineDisplay`, vm.createContext(state))
  assert.equal(display(2), 'Business PostgreSQL · postgresql (#2)')
  state.engines.value[0].name = 'Renamed database'
  assert.match(display(2), /^Renamed database/)
  assert.equal(display(99), 'service.query.engineUnavailable:99')
  state.enginesLoading.value = true
  assert.equal(display(2), 'service.query.engineLoading:')
  state.enginesLoading.value = false
  state.enginesUnavailable.value = true
  assert.equal(display(2), 'service.query.engineUnavailable:2')
})

const revisionSource = source.slice(source.indexOf('const auth = useAuthStore()'), source.indexOf('const metricBindingRequired ='))
const revisionMessages = Object.fromEntries(await Promise.all(['zh-cn', 'en'].map(async locale => [locale, JSON.parse(await readFile(new URL(`../src/i18n/${locale}.json`, import.meta.url), 'utf8'))])))
function revisionHarness(get, canRead = true) {
  const scope = effectScope()
  const snapshot = ref({ metric_source: { implementation_id: 1, revision_id: 6 } })
  const permission = ref(canRead)
  const i18n = createI18n({ legacy: false, locale: 'zh-cn', messages: revisionMessages })
  const state = scope.run(() => vm.runInNewContext(`${revisionSource}; ({ metricRevisionLabel, metricRevisionNo, metricRevisionLoading })`, {
    ref, computed, watch, sourceSnapshot: snapshot,
    useAuthStore: () => ({ hasPermission: () => permission.value }),
    createModelMetricAPI: () => ({ get }), client: {}, t: (key, params) => i18n.global.t(key, { ...params })
  }))
  return { state, snapshot, permission, i18n, stop: () => scope.stop() }
}
const settleRevision = () => new Promise(resolve => setImmediate(resolve))

test('bound revision display resolves ID 6 to R2, never the newest revision, in both languages', async () => {
  const h = revisionHarness(async id => {
    assert.equal(id, 1)
    return { revisions: [{ id: 9, revision_no: 3 }, { id: 6, revision_no: 2 }] }
  })
  try {
    assert.equal(h.state.metricRevisionLabel.value, '正在读取修订号（修订 ID：6）')
    await settleRevision()
    assert.equal(h.state.metricRevisionLabel.value, 'R2（修订 ID：6）')
    h.i18n.global.locale.value = 'en'
    assert.equal(h.state.metricRevisionLabel.value, 'R2 (revision ID: 6)')
  } finally { h.stop() }
})

test('missing permission, missing revision and API failure never invent a revision number', async () => {
  const cases = [
    [() => assert.fail('must not request Model without read permission'), false],
    [async () => ({ revisions: [{ id: 9, revision_no: 3 }] }), true],
    [async () => { throw new Error('unavailable') }, true]
  ]
  for (const [get, permission] of cases) {
    const h = revisionHarness(get, permission)
    try {
      await settleRevision()
      assert.equal(h.state.metricRevisionLabel.value, '修订号不可用（修订 ID：6）')
      assert.equal(h.state.metricRevisionLoading.value, false)
    } finally { h.stop() }
  }
})

test('switching source or losing permission discards late revision responses', async () => {
  const pending = []
  const h = revisionHarness(() => new Promise(resolve => pending.push(resolve)))
  try {
    h.snapshot.value = { metric_source: { implementation_id: 2, revision_id: 8 } }
    pending[1]({ revisions: [{ id: 8, revision_no: 4 }] })
    await settleRevision()
    pending[0]({ revisions: [{ id: 6, revision_no: 2 }] })
    await settleRevision()
    assert.equal(h.state.metricRevisionLabel.value, 'R4（修订 ID：8）')
    h.permission.value = false
    assert.equal(h.state.metricRevisionLabel.value, '修订号不可用（修订 ID：8）')
    h.permission.value = true
    h.stop()
    pending[2]({ revisions: [{ id: 8, revision_no: 4 }] })
    await settleRevision()
    assert.equal(h.state.metricRevisionNo.value, null)
  } finally { h.stop() }
})
