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

test('metric rebind sends an exact revision with the current version and clears old preview state', async () => {
  const requests = []
  const state = {
    sourceSnapshot: { value: { metric_source: { implementation_id: 4, revision_id: 16, result_kind: 'summary' } } },
    rebindSelection: { value: null }, rebindDialogOpen: { value: false }, rebindSaving: { value: false },
    service: { value: { id: 41, version: 3, status: 'active' } }, serviceId: { value: 41 }, versionConflict: { value: false },
    previewData: { value: [{ group_key: 'old' }] },
    previewPagination: { value: { page: 2, hasMore: true, nextCursor: 'old', cursors: ['', 'old'] } },
    metricRevisionRefresh: { value: 0 },
    queryServiceAPI: { rebindMetricSource: async (id, body) => {
      requests.push({ id, body: JSON.parse(JSON.stringify(body)) })
      return { id, version: body.version + 1, status: 'active', data_config: { source_snapshot: { metric_source: body.metric_source } } }
    } },
    captureVersionConflict: () => false, ElMessage: { success() {}, error() {} }, t: key => key
  }
  const rebindSource = source.slice(source.indexOf('const openRebindDialog ='), source.indexOf('const checkSourceSnapshot ='))
  const { openRebindDialog, rebindMetricSource } = vm.runInNewContext(`${rebindSource}\n({ openRebindDialog, rebindMetricSource })`, state)
  openRebindDialog()
  assert.equal(state.rebindSelection.value.revision_id, 16)
  state.rebindSelection.value = { implementation_id: 4, revision_id: 19 }
  await rebindMetricSource()
  assert.deepEqual(requests, [{ id: 41, body: { metric_source: { implementation_id: 4, revision_id: 19 }, version: 3 } }])
  assert.equal(state.service.value.version, 4)
  assert.equal(state.rebindDialogOpen.value, false)
  assert.equal(state.previewData.value.length, 0)
  assert.equal(state.previewPagination.value.nextCursor, '')
  assert.equal(state.metricRevisionRefresh.value, 1)
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
  const service = ref({ config_type: 'analytical', status: 'active', version: 3 })
  const i18n = createI18n({ legacy: false, locale: 'zh-cn', messages: revisionMessages })
  const state = scope.run(() => vm.runInNewContext(`${revisionSource}; ({ metricRevisionLabel, metricRevisionNo, metricRevisionLoading, metricRevisionState, metricRevisionRefresh, metricRevisionStatusType })`, {
    ref, computed, watch, sourceSnapshot: snapshot, service,
    useAuthStore: () => ({ hasPermission: () => permission.value }),
    createModelMetricAPI: () => ({ get }), client: {}, t: (key, params) => i18n.global.t(key, { ...params })
  }))
  return { state, snapshot, permission, service, i18n, stop: () => scope.stop() }
}
const settleRevision = () => new Promise(resolve => setImmediate(resolve))

test('bound revision display resolves ID 6 to R2, never the newest revision, in both languages', async () => {
  const h = revisionHarness(async id => {
    assert.equal(id, 1)
    return { revisions: [{ id: 9, revision_no: 3, status: 'published' }, { id: 6, revision_no: 2, status: 'withdrawn' }] }
  })
  try {
    assert.equal(h.state.metricRevisionLabel.value, '正在读取修订号（修订 ID：6）')
    await settleRevision()
    assert.equal(h.state.metricRevisionLabel.value, 'R2（修订 ID：6）')
    assert.equal(h.state.metricRevisionState.value, 'withdrawn')
    assert.equal(h.state.metricRevisionStatusType.value, 'warning')
    assert.match(h.i18n.global.t('service.query.metricRevisionHint_withdrawn'), /新查询也会被拒绝/)
    h.i18n.global.locale.value = 'en'
    assert.equal(h.state.metricRevisionLabel.value, 'R2 (revision ID: 6)')
    assert.match(h.i18n.global.t('service.query.metricRevisionHint_withdrawn'), /New queries will be rejected/)
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

test('revision status distinguishes permission, missing revision and unavailable Model responses', async () => {
  const cases = [
    [() => assert.fail('must not request without permission'), false, 'forbidden'],
    [async () => { throw { response: { status: 403 } } }, true, 'forbidden'],
    [async () => { throw { response: { status: 404 } } }, true, 'missing'],
    [async () => ({ revisions: [{ id: 9, revision_no: 3, status: 'published' }] }), true, 'missing'],
    [async () => { throw { response: { status: 503 } } }, true, 'unavailable'],
    [async () => { throw new Error('offline') }, true, 'unavailable'],
    [async () => ({ revisions: [{ id: 6, revision_no: 2, status: 'unexpected' }] }), true, 'unavailable']
  ]
  for (const [get, permission, expected] of cases) {
    const h = revisionHarness(get, permission)
    try {
      await settleRevision()
      assert.equal(h.state.metricRevisionState.value, expected)
      assert.equal(h.state.metricRevisionLoading.value, false)
      assert.equal(h.service.value.status, 'active', 'supplemental read must not change service state')
      for (const lang of ['zh-cn', 'en']) {
        const messages = revisionMessages[lang].service.query
        assert.ok(messages[`metricRevisionStatus_${expected}`])
        assert.ok(messages[`metricRevisionHint_${expected}`])
      }
    } finally { h.stop() }
  }
})

test('refresh reads withdrawal without changing the binding, service or entered parameters', async () => {
  let status = 'published', calls = 0
  const h = revisionHarness(async () => { calls++; return { revisions: [{ id: 6, revision_no: 2, status }] } })
  try {
    h.service.value.parameters = { subject_id: 'user-entered', grain: 'month' }
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'published')
    assert.equal(h.state.metricRevisionStatusType.value, 'info')
    const before = JSON.stringify({ service: h.service.value, snapshot: h.snapshot.value })
    status = 'withdrawn'
    h.state.metricRevisionRefresh.value++
    assert.equal(h.state.metricRevisionState.value, 'loading')
    assert.equal(h.state.metricRevisionNo.value, null)
    await settleRevision()
    assert.equal(calls, 2)
    assert.equal(h.state.metricRevisionState.value, 'withdrawn')
    assert.equal(JSON.stringify({ service: h.service.value, snapshot: h.snapshot.value }), before)
    status = 'draft'
    h.state.metricRevisionRefresh.value++
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'draft')
    assert.equal(h.state.metricRevisionStatusType.value, 'warning')
    assert.match(source, /@click="metricRevisionRefresh\+\+"/)
    assert.match(source, /metricRevisionHint_\$\{metricRevisionState\}/)
  } finally { h.stop() }
})

test('refresh errors discard the old published state and can be explicitly retried', async () => {
  let fail = false
  const h = revisionHarness(async () => {
    if (fail) throw new Error('unavailable')
    return { revisions: [{ id: 6, revision_no: 2, status: 'published' }] }
  })
  try {
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'published')
    fail = true
    h.state.metricRevisionRefresh.value++
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'unavailable')
    assert.equal(h.state.metricRevisionNo.value, null)
    fail = false
    h.state.metricRevisionRefresh.value++
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'published')
  } finally { h.stop() }
})

test('permission changes clear status, ignore stale reads and reload when restored', async () => {
  const pending = []
  const h = revisionHarness(() => new Promise(resolve => pending.push(resolve)))
  try {
    h.permission.value = false
    assert.equal(h.state.metricRevisionState.value, 'forbidden')
    pending[0]({ revisions: [{ id: 6, revision_no: 2, status: 'published' }] })
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'forbidden')
    assert.equal(h.state.metricRevisionNo.value, null)
    h.permission.value = true
    pending[1]({ revisions: [{ id: 6, revision_no: 2, status: 'withdrawn' }] })
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'withdrawn')
  } finally { h.stop() }
})

test('a stale failure after rebind cannot overwrite the current revision state', async () => {
  const pending = []
  const h = revisionHarness(() => new Promise((resolve, reject) => pending.push({ resolve, reject })))
  try {
    h.snapshot.value = { metric_source: { implementation_id: 2, revision_id: 8 } }
    pending[1].resolve({ revisions: [{ id: 8, revision_no: 4, status: 'published' }] })
    await settleRevision()
    pending[0].reject({ response: { status: 404 } })
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'published')
    assert.equal(h.state.metricRevisionLabel.value, 'R4（修订 ID：8）')
    h.state.metricRevisionRefresh.value++
    h.stop()
    pending[2].resolve({ revisions: [{ id: 8, revision_no: 4, status: 'withdrawn' }] })
    await settleRevision()
    assert.equal(h.state.metricRevisionState.value, 'loading', 'unmounted state must not accept late results')
  } finally { h.stop() }
})

test('ordinary table and SQL configurations do not request Model even with an old source reference', async () => {
  let calls = 0
  const h = revisionHarness(async () => { calls++; return { revisions: [] } }, false)
  try {
    h.service.value.config_type = 'table'
    h.permission.value = true
    h.state.metricRevisionRefresh.value++
    await settleRevision()
    h.service.value.config_type = 'sql'
    h.state.metricRevisionRefresh.value++
    await settleRevision()
    assert.equal(calls, 0)
    h.service.value.config_type = 'analytical'
    h.snapshot.value = null
    await settleRevision()
    const before = calls
    h.state.metricRevisionRefresh.value++
    await settleRevision()
    assert.equal(calls, before, 'unbound metrics have no Model read to perform')
  } finally { h.stop() }
})
