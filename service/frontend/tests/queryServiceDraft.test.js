import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import * as Vue from 'vue'
import { parse, compileScript } from '@vue/compiler-sfc'
import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'

const source = readFileSync(new URL('../src/views/QueryServiceForm.vue', import.meta.url), 'utf8')
let code = compileScript(parse(source).descriptor, { id: 'query-draft' }).content
code = code.replace(/^import\s+([\s\S]*?)\s+from\s+['"]([^'"]+)['"];?$/gm, (_, bindings, path) =>
  `const ${bindings.startsWith('{') ? bindings : `{ default: ${bindings} }`} = modules[${JSON.stringify(path)}]`)
  .replace('export default', 'return')
const create = new Function('modules', code)

function harness(api = {}, params = {}, metadata = async () => ({ has_geometry: false }), canBrowseMeta = true) {
  const route = Vue.reactive({ params, name: params.id ? 'QueryServiceEdit' : 'QueryServiceCreate', query: {} })
  let guard, pendingLoad, unmount
  const scope = Vue.effectScope()
  const mounted = [], navigations = [], messages = []
  const modules = {
    vue: { ...Vue, onMounted: callback => mounted.push(callback), onBeforeUnmount: callback => { unmount = callback },
      watch: (source, callback, options) => Vue.watch(source, (...args) => { pendingLoad = callback(...args) }, options) },
    'vue-router': { useRouter: () => ({}), useRoute: () => route },
    'vue-i18n': { useI18n: () => ({ t: key => key }) },
    'element-plus': { ElMessage: Object.fromEntries(['success', 'warning', 'error'].map(type => [type, message => messages.push({ type, message })])), ElMessageBox: {} },
    '@element-plus/icons-vue': {},
    '@/api/queryService': { default: { getStorageEngines: async () => [], ...api } },
    '@common-ui': { createLatestRequestCoordinator, withTransientRetry: fn => fn(), useUnsavedChangesGuard: options => { guard = options },
      detectTableMetadata: metadata, locatorPathFromSelection: selection => selection.path, isEngineSelectable: () => true },
    '@/utils/resourceSelection': {},
    '@/utils/queryServiceEngines': { queryServiceExecutionEngines: value => value, federatedQueryRuntimes: value => value, tableSelectionUsesRuntime: () => false },
    '@/utils/serviceHelper': { SERVICE_NAME_PATTERN: /.+/ },
    '@/store/auth': { useAuthStore: () => ({ hasPermission: permission => canBrowseMeta && permission === 'meta.catalog.read' }) },
    '@/utils/moduleNavigation': { navigateServiceRoute: async (_router, path) => { navigations.push({ path, dirty: guard.isDirty() }) } },
    '../components/PublishedMetricSourcePicker.vue': {},
    '../../../../common-frontend/basic/src/components/ParameterValueInput.vue': {},
    '../../../../common-frontend/basic/src/utils/parameterInput.mjs': {}
  }
  const state = scope.run(() => create(modules).setup({}, { expose() {} }))
  return { state, guard, route, navigations, messages,
    async mount() { const initialLoad = pendingLoad; await Promise.all(mounted.map(callback => callback())); await initialLoad },
    async switchTo(id) { route.params = id ? { id: String(id) } : {}; route.name = id ? 'QueryServiceEdit' : 'QueryServiceCreate'; await Vue.nextTick(); await pendingLoad },
    unmount() { unmount(); scope.stop() }
  }
}
const service = id => ({ id, version: 2, service_name: `service_${id}`, title: `Service ${id}`, config_type: 'sql', sql_query: 'SELECT id FROM sample', named_parameters: [], data_config: {}, protocols: { rest_api: { enabled: true } } })

test('query creation starts in SQL mode when Meta catalog is unavailable', () => {
  const h = harness({}, {}, async () => ({ has_geometry: false }), false)
  assert.equal(h.state.form.config_type, 'sql')
  h.unmount()
})

test('query draft protects SQL, nested parameters, source and publishing inputs; UI state stays clean', async () => {
  const h = harness()
  await h.mount()
  assert.equal(h.guard.isDirty(), false)
  const s = h.state
  for (const change of [
    () => { s.form.sql_query = 'SELECT :id' },
    () => { s.form.locator = 'source-table' },
    () => { s.form.config_type = 'analytical' },
    () => { s.form.title = 'Edited' },
    () => { s.sqlNamedParameters.value = [{ name: 'id', options: [{ value: 1, labels: { en: 'One' } }] }] },
    () => { s.sqlNamedParameters.value[0].options[0].labels.en = 'First' },
    () => { s.sqlStableKey.value = ['id'] },
    () => { s.sqlGeometryColumn.value = 'shape' },
    () => { s.defaultFieldsInput.value = 'id,title' },
    () => { s.filterableFieldsInput.value = 'id' },
    () => { s.metricSource.value = { implementation_id: 3, revision_id: 4 } },
    () => { s.enableOgcFeatures.value = true },
    () => { s.inputValue.value = 'unfinished keyword' }
  ]) {
    change()
    assert.equal(h.guard.isDirty(), true)
    s.markSaved()
    assert.equal(h.guard.isDirty(), false)
  }
  s.currentStep.value = 2
  s.sqlStableKeyFilter.value = 'search'
  s.inputVisible.value = true
  s.engines.value = [{ id: 1 }]
  s.form.version = 3
  assert.equal(h.guard.isDirty(), false)
  assert.doesNotMatch(source, /beforeunload|localStorage|sessionStorage|onBeforeRouteLeave/)
})

test('initial async discovery cannot mark user input as saved; reverting restores clean state', async () => {
  let finish
  const h = harness({ getStorageEngines: () => new Promise(resolve => { finish = resolve }) })
  const mounted = h.mount()
  h.state.form.sql_query = 'SELECT id FROM sample'
  finish([])
  await mounted
  assert.equal(h.guard.isDirty(), true)
  h.state.form.sql_query = ''
  assert.equal(h.guard.isDirty(), false)
})

test('loaded edit is clean and save success clears dirty state before navigating', async () => {
  const h = harness({ getService: async () => service(41), updateService: async () => ({ version: 3 }) }, { id: '41' })
  await h.mount()
  assert.equal(h.guard.isDirty(), false)
  h.state.form.title = 'Updated'
  await h.state.handleSubmit()
  assert.equal(h.state.form.version, 3)
  assert.deepEqual(h.navigations, [{ path: '/query-services', dirty: false }])
})

const roleChoice = { target: { owner_module: 'meta', resource_type: 'data_item', resource_identity: 'source-a' }, component_key: 'phone', selection: { policy_id: 'policy', policy_revision: 1, assessment_revision: 1, option_id: 'option' } }
const roleService = id => ({ ...service(id), config_type: 'table', engine_id: 11, data_config: { locator: 'source-table', protection_choices: [structuredClone(roleChoice)] } })

test('role choice is loaded and saved once with the service, and explicit removal restores defaults', async () => {
  let saved
  const h = harness({ getService: async () => roleService(41), updateService: async (_id, request) => { saved = request; return { version: 3 } } }, { id: '41' })
  await h.mount()
  assert.deepEqual(JSON.parse(JSON.stringify(h.state.form.protection_choices)), [roleChoice])
  assert.equal(h.guard.isDirty(), false)
  h.state.form.title = 'Role service'
  await h.state.handleSubmit()
  assert.deepEqual(saved.data_config.protection_choices, [roleChoice])
  assert.equal(saved.data_config.protection_choices[0].algorithm, undefined)
  h.state.form.protection_choices = []
  assert.equal(h.guard.isDirty(), true)
  await h.state.handleSubmit()
  assert.deepEqual(saved.data_config.protection_choices, [])
  h.unmount()
})

test('unavailable candidates and failed saves retain saved role choices and the current version', async () => {
  const h = harness({ getService: async () => roleService(41), getProtectionOptions: async () => { throw { response: { data: { error: 'current method unavailable' } } } }, updateService: async () => { throw { response: { data: { error_code: 'query_protection_choice_invalid', error: 'choice changed' } } } } }, { id: '41' })
  await h.mount()
  await h.state.refreshMaskingOptions()
  assert.equal(h.state.maskingError.value, 'current method unavailable')
  assert.deepEqual(JSON.parse(JSON.stringify(h.state.form.protection_choices)), [roleChoice])
  h.state.form.title = 'Unsaved'
  await h.state.handleSubmit()
  assert.equal(h.state.form.version, 2)
  assert.equal(h.guard.isDirty(), true)
  assert.equal(h.state.form.protection_choices.length, 1)
  h.unmount()
})

test('source and editor changes invalidate pending candidates without silently changing the selection', async () => {
  let finish
  const h = harness({ getService: async id => roleService(id), getProtectionOptions: () => new Promise(resolve => { finish = resolve }) }, { id: '41' })
  await h.mount()
  const pending = h.state.refreshMaskingOptions()
  h.state.form.locator = 'another-source'
  await Vue.nextTick()
  finish({ fields: [{ component_key: 'stale-source-field', options: [] }] })
  await pending
  assert.equal(h.state.maskingFields.value.length, 0)
  assert.equal(h.state.maskingLoading.value, false)
  assert.equal(h.state.form.protection_choices.length, 1)
  await h.switchTo(42)
  assert.equal(h.guard.isDirty(), false)
  h.state.form.config_type = 'sql'
  await Vue.nextTick()
  await h.state.refreshMaskingOptions()
  assert.equal(h.state.maskingError.value, 'service.query.maskingUnsupported')
  assert.equal(h.state.form.protection_choices.length, 1)
  h.unmount()
})

test('the service form reuses the sole shared field masking component', () => {
  assert.match(source, /FieldMaskingChoices,/)
  assert.match(source, /<FieldMaskingChoices[\s\S]*?v-model="form\.protection_choices"/)
  assert.doesNotMatch(source, /option_id\s*[:=]|policy_revision\s*[:=]|role_keys\s*[:=]/)
  let found = 0
  const visit = (node, forms = 0) => {
    if (node.tag === 'FieldMaskingChoices') { assert.equal(forms, 0, 'shared masking form must not be nested inside another form'); found++ }
    for (const child of node.children || []) visit(child, forms + (node.tag === 'el-form' ? 1 : 0))
  }
  visit(parse(source).descriptor.template.ast)
  assert.equal(found, 1)
})

for (const conflict of [false, true]) {
  test(`failed save preserves SQL and parameters (version conflict: ${conflict})`, async () => {
    const h = harness({ createService: async () => { throw { response: { data: { error_code: conflict ? 'resource_version_conflict' : 'unavailable' } } } } })
    await h.mount()
    h.state.form.config_type = 'sql'
    h.state.form.sql_query = 'SELECT :count'
    h.state.sqlNamedParameters.value = [{ name: 'count', value: 1 }]
    await h.state.handleSubmit()
    assert.equal(h.guard.isDirty(), true)
    assert.equal(h.state.form.sql_query, 'SELECT :count')
    assert.equal(h.state.sqlNamedParameters.value[0].value, 1)
    assert.equal(h.state.submitting.value, false)
    assert.deepEqual(h.navigations, [])
  })
}

test('identity switching clears draft and ignores old loads and saves', async () => {
  let finishLoad, finishSave
  const h = harness({ getService: async id => id === '41' ? new Promise(resolve => { finishLoad = resolve }) : service(id),
    updateService: () => new Promise(resolve => { finishSave = resolve }) }, { id: '41' })
  const mounted = h.mount()
  await h.switchTo(42)
  finishLoad(service(41))
  await mounted
  assert.equal(h.state.form.title, 'Service 42')
  assert.equal(h.guard.isDirty(), false)
  h.state.form.title = 'Updating 42'
  const saved = h.state.handleSubmit()
  await h.switchTo(43)
  finishSave({ version: 9 })
  await saved
  assert.equal(h.state.form.title, 'Service 43')
  assert.equal(h.state.form.version, 2)
  assert.deepEqual(h.navigations, [])
  await h.switchTo(null)
  assert.equal(h.state.form.sql_query, '')
  assert.equal(h.guard.isDirty(), false)
  assert.equal(h.guard.shouldConfirmUpdate({ name: 'QueryServiceEdit', params: { id: '42' } }, { name: 'QueryServiceEdit', params: { id: '41' } }), true)
})

test('late table metadata cannot change a new editor or newer source selection', async () => {
  const pending = []
  const h = harness({ getService: async id => service(id) }, {}, () => new Promise(resolve => pending.push(resolve)))
  await h.mount()
  const selection = id => ({ path: ['public', `table_${id}`], identity: { engine_id: 1, locator: `table-${id}` } })
  const first = h.state.handleTableSelection(selection(1))
  const second = h.state.handleTableSelection(selection(2))
  pending[1]({ has_geometry: false })
  await second
  pending[0]({ has_geometry: true, geometry_column: 'shape', srid: 4326 })
  await first
  assert.equal(h.state.enableOgcFeatures.value, false)
  const third = h.state.handleTableSelection(selection(3))
  await h.switchTo(41)
  await h.switchTo(null)
  pending[2]({ has_geometry: true, geometry_column: 'shape', srid: 4326 })
  await third
  assert.equal(h.guard.isDirty(), false)
})

test('query-only navigation keeps the draft for the same service identity', async () => {
  let loads = 0
  const h = harness({ getService: async id => { loads++; return service(id) } }, { id: '41' })
  await h.mount()
  h.state.form.title = 'Unsaved title'
  h.route.params = { id: '41' }
  h.route.query = { view: 'details' }
  await Vue.nextTick()
  assert.equal(loads, 1)
  assert.equal(h.state.form.title, 'Unsaved title')
  assert.equal(h.guard.isDirty(), true)
  assert.equal(h.guard.shouldConfirmUpdate(h.route, h.route), false)
})

test('creation saves only the submitted snapshot and blocks duplicate requests', async () => {
  for (const lateChange of [false, true]) {
    let finish, calls = 0
    const h = harness({ createService: () => { calls++; return new Promise(resolve => { finish = resolve }) } })
    await h.mount()
    h.state.form.title = 'Submitted title'
    const saving = h.state.handleSubmit()
    await h.state.handleSubmit()
    assert.equal(calls, 1)
    if (lateChange) h.state.form.title = 'Later input'
    finish({ id: 41 })
    await saving
    assert.deepEqual(h.navigations, [{ path: '/query-services/41', dirty: lateChange }])
    assert.equal(h.guard.isDirty(), lateChange)
  }
})
