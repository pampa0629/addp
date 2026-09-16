import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import test from 'node:test'

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
