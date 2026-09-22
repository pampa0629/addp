import assert from 'node:assert/strict'
import test from 'node:test'
import { existsSync, readFileSync } from 'node:fs'
import { createRouter, createMemoryHistory } from 'vue-router'
import { parse } from '@vue/compiler-sfc'
import { reactive, ref, toRefs } from 'vue'

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')

function component(name, api = {}, confirm = async () => {}) {
  const { descriptor } = parse(read(`../src/views/${name}.vue`))
  const code = descriptor.script.content.replace(/^import .*$/gm, '').replace('export default', 'return')
  const navigations = []
  const navigationDirtyStates = []
  const messages = []
  const confirmations = []
  const message = options => messages.push(options)
  for (const type of ['success', 'error', 'warning', 'info']) message[type] = text => messages.push({ type, message: text })
  let leaveGuard
  const definition = new Function('registeredServiceAPI', 'navigateServiceRoute', 'ElMessage', 'ElMessageBox', 'alert', 'reactive', 'ref', 'toRefs', 'useRouter', 'useUnsavedChangesGuard', 'publishConsolePageDescriptor', code)(
    api, (_router, path, options) => { navigationDirtyStates.push(leaveGuard?.isDirty()); navigations.push({ path, options }) }, message,
    { confirm: (...args) => { confirmations.push(args); return confirm(...args) } },
    () => assert.fail('native alert must not be used'), reactive, ref, toRefs, () => ({}), options => { leaveGuard = options }, async () => {})
  const state = reactive({ ...definition.data(), ...definition.setup?.(), $router: {}, $route: name === 'RegisteredServiceDetail' ? { path: '/services/42', params: { id: '42' } } : { path: '/services/create', params: {} }, $t: key => key })
  for (const [name, method] of Object.entries(definition.methods)) state[name] = method.bind(state)
  return { state, navigations, messages, confirmations, leaveGuard, definition, navigationDirtyStates }
}

test('Console registry routes resolve to the only registered-service pages', () => {
  const source = read('../src/router/index.js')
  const code = source.slice(source.indexOf('const routes ='), source.indexOf('const router ='))
  const routes = new Function('Layout', `${code}; return routes`)({})
  const router = createRouter({ history: createMemoryHistory(), routes })
  for (const [path, name] of [
    ['/services', 'RegisteredServiceList'], ['/services/create', 'RegisteredServiceCreate'],
    ['/services/42', 'RegisteredServiceDetail'], ['/services/42/edit', 'RegisteredServiceEdit']
  ]) {
    assert.equal(router.resolve(path).name, name)
  }
  assert.equal(router.getRoutes().some(route => route.path.startsWith('/registered-services')), false)
  for (const file of ['ServiceManagement.vue', 'ServiceForm.vue', 'ServiceDetail.vue']) {
    assert.equal(existsSync(new URL(`../src/views/${file}`, import.meta.url)), false, `${file} must be removed`)
  }
  assert.equal(existsSync(new URL('../src/api/service.js', import.meta.url)), false)
})

test('cancelled navigation preserves the current document title', async () => {
  const source = read('../src/router/index.js')
  const hook = source.slice(source.indexOf('router.afterEach'), source.indexOf('export default router'))
  const document = { title: '' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/services/create', component: {}, meta: { title: '注册外部服务' } },
      { path: '/services', component: {}, meta: { title: '注册服务' } }
    ]
  })
  new Function('router', 'document', 'DEFAULT_TITLE', hook)(router, document, '数据服务')
  await router.push('/services/create')
  assert.equal(document.title, '注册外部服务')
  const removeGuard = router.beforeEach(() => false)
  await router.push('/services')
  assert.equal(router.currentRoute.value.fullPath, '/services/create')
  assert.equal(document.title, '注册外部服务')
  removeGuard()
  await router.push('/services')
  assert.equal(document.title, '注册服务')
})

test('registration list uses canonical search/pagination and Console navigation', async () => {
  let params
  const { state, navigations } = component('RegisteredServiceList', {
    listServices: async value => { params = value; return { data: [{ id: 42 }], total: 31 } }
  })
  state.searchQuery = 'weather'
  state.page = 2
  await state.loadServices()
  assert.deepEqual(params, { page: 2, limit: 20, search: 'weather' })
  assert.equal(state.total, 31)
  state.goToCreate()
  state.goToDetail(42)
  state.goToEdit(42)
  assert.deepEqual(navigations.map(item => item.path), ['/services/create', '/services/42', '/services/42/edit'])
})

test('creation replaces the form URL with the returned service identity', async () => {
  let request
  const { state, navigations } = component('RegisteredServiceForm', {
    createService: async data => { request = data; return { id: 42 } }
  })
  Object.assign(state.form, { service_name: 'weather', title: 'Weather', service_type: 'rest', endpoint_url: 'https://example.test/weather' })
  await state.handleSubmit()
  assert.equal(request.service_name, 'weather')
  assert.equal(request.endpoint_url, 'https://example.test/weather')
  assert.deepEqual(navigations, [{ path: '/services/42', options: { history: 'replace' } }])
})

test('editing reads canonical fields and returns to the same service detail', async () => {
  let request
  const { state, navigations } = component('RegisteredServiceForm', {
    getService: async () => ({ id: 42, service_name: 'weather', title: 'Weather', service_type: 'rest', endpoint_url: 'https://example.test/weather', auth_type: 'none' }),
    updateService: async (id, data) => { request = { id, data } }
  })
  state.isEdit = true
  state.serviceId = 42
  await state.loadService()
  assert.equal(state.form.service_name, 'weather')
  assert.equal(state.form.endpoint_url, 'https://example.test/weather')
  state.form.title = 'Updated weather'
  await state.handleSubmit()
  assert.equal(request.id, 42)
  assert.equal(request.data.title, 'Updated weather')
  assert.equal('service_name' in request.data, false)
  assert.equal('endpoint_url' in request.data, false)
  assert.deepEqual(navigations, [{ path: '/services/42', options: { history: 'replace' } }])
})

test('catalog and registration pages never navigate through a second route family', () => {
  for (const file of ['ServiceCatalog', 'RegisteredServiceList', 'RegisteredServiceForm', 'RegisteredServiceDetail']) {
    assert.doesNotMatch(read(`../src/views/${file}.vue`), /\/registered-services/)
  }
  const catalog = read('../src/views/ServiceCatalog.vue')
  assert.match(catalog, /`\/services\/\$\{service.id\}`/)
  assert.match(catalog, /navigateServiceRoute\(router, '\/services'\)/)
})


test('registration pages use platform feedback instead of native dialogs', () => {
  for (const file of ['RegisteredServiceList', 'RegisteredServiceForm', 'RegisteredServiceDetail']) {
    assert.doesNotMatch(read(`../src/views/${file}.vue`), /(?<![.\w])(?:alert|confirm)\(/)
  }
})

for (const [name, action, args] of [
  ['RegisteredServiceList', 'confirmDelete', [42]],
  ['RegisteredServiceDetail', 'handleDelete', []],
  ['RegisteredServiceDetail', 'refreshMetadata', []]
]) {
  for (const dismissal of ['cancel', 'close']) {
    test(`${name}.${action}: ${dismissal} performs no request or error notification`, async () => {
      const api = new Proxy({}, { get: () => () => assert.fail('cancelled action sent a request') })
      const { state, messages, navigations, confirmations } = component(name, api, async () => { throw dismissal })
      state.service = { id: 42 }
      await state[action](...args)
      assert.equal(confirmations.length, 1)
      const options = confirmations[0][2]
      assert.equal(options.customClass, 'addp-message-box')
      assert.equal(options.cancelButtonText, 'service.common.cancel')
      assert.ok(options.confirmButtonText)
      if (action !== 'refreshMetadata') {
        assert.equal(options.confirmButtonClass, 'el-button--danger')
        assert.equal(options.autofocus, false)
      }
      assert.deepEqual(messages, [])
      assert.deepEqual(navigations, [])
      assert.equal(action === 'refreshMetadata' ? state.refreshing : state.deleting, false)
    })
  }
}

test('pending deletion only opens one confirmation and sends one request after approval', async () => {
  let approve
  let deletes = 0
  let reloads = 0
  const { state, messages, confirmations } = component('RegisteredServiceList', {
    deleteService: async id => { assert.equal(id, 42); deletes++ }
  }, () => new Promise(resolve => { approve = resolve }))
  state.loadServices = async () => { reloads++ }
  const pending = state.confirmDelete(42)
  await state.confirmDelete(42)
  assert.equal(confirmations.length, 1)
  assert.equal(deletes, 0)
  approve()
  await pending
  assert.equal(deletes, 1)
  assert.equal(reloads, 1)
  assert.equal(state.deleting, false)
  assert.equal(messages[0].type, 'success')
})

test('failed deletion stays on the detail page and reports an error', async () => {
  const { state, messages, navigations } = component('RegisteredServiceDetail', {
    deleteService: async () => { throw new Error('request failed') }
  })
  state.service = { id: 42 }
  await state.handleDelete()
  assert.deepEqual(navigations, [])
  assert.equal(state.deleting, false)
  assert.equal(messages[0].type, 'error')
  assert.match(messages[0].message, /request failed/)
})

test('failed saving preserves the form and releases the submitting state', async () => {
  const { state, messages, navigations } = component('RegisteredServiceForm', {
    createService: async () => { throw new Error('request failed') }
  })
  state.form.title = 'Weather'
  await state.handleSubmit()
  assert.equal(state.form.title, 'Weather')
  assert.equal(state.submitting, false)
  assert.deepEqual(navigations, [])
  assert.equal(messages[0].type, 'error')
})

test('metadata refresh reloads after the synchronous API result without a timer', async () => {
  const events = []
  const { state, messages } = component('RegisteredServiceDetail', {
    refreshMetadata: async (id, body) => { assert.equal(id, 42); assert.deepEqual(body, { force: true }); events.push('refresh') }
  })
  state.service = { id: 42 }
  state.loadService = async () => { events.push('reload') }
  await state.refreshMetadata()
  assert.deepEqual(events, ['refresh', 'reload'])
  assert.equal(state.refreshing, false)
  assert.equal(messages[0].type, 'success')
})

for (const name of ['RegisteredServiceList', 'RegisteredServiceDetail']) {
  test(`${name}: health status determines the nonblocking feedback severity`, async () => {
    for (const [status, type] of [['healthy', 'success'], ['unhealthy', 'warning']]) {
      const { state, messages } = component(name, { healthCheck: async () => ({ status, message: 'result', response_time: 12 }) })
      state.service = { id: 42 }
      let reloads = 0
      state.loadService = state.loadServices = async () => { reloads++ }
      await state.healthCheck(42)
      assert.equal(messages[0].type, type)
      assert.equal(reloads, 1)
    }
  })
}


test('registration draft tracks fields, keywords and credentials without custom navigation guards', () => {
  const { state, leaveGuard } = component('RegisteredServiceForm')
  assert.ok(leaveGuard, 'must register the shared guard')
  assert.equal(leaveGuard.isDirty(), false)
  assert.equal(leaveGuard.shouldConfirmUpdate({ name: 'edit', params: { id: '42' } }, { name: 'edit', params: { id: '41' } }), true)
  assert.equal(leaveGuard.shouldConfirmUpdate({ name: 'edit', params: { id: '42' } }, { name: 'edit', params: { id: '42' } }), false)
  for (const [target, key, value] of [
    [state.form, 'title', 'Weather'], [state, 'keywordsInput', 'weather'],
    [state.authConfig, 'token', 'draft-token'], [state.form, 'auto_refresh_metadata', false]
  ]) {
    const before = target[key]
    target[key] = value
    assert.equal(leaveGuard.isDirty(), true)
    target[key] = before
    assert.equal(leaveGuard.isDirty(), false)
  }
  const source = read('../src/views/RegisteredServiceForm.vue')
  assert.doesNotMatch(source, /beforeunload|ElMessageBox|beforeRouteLeave|localStorage|sessionStorage/)
})

test('loading an existing service establishes a clean draft, including empty credentials', async () => {
  const { state, leaveGuard } = component('RegisteredServiceForm', {
    getService: async () => ({ id: 42, service_name: 'weather', title: 'Weather', service_type: 'rest', endpoint_url: 'https://example.test', keywords: ['weather'], auth_type: 'bearer' })
  })
  state.serviceId = 42
  state.isEdit = true
  await state.loadService()
  assert.equal(leaveGuard.isDirty(), false)
  state.authConfig.token = 'replacement-token'
  assert.equal(leaveGuard.isDirty(), true)
})

test('save success clears the draft before navigation; failure preserves the dirty draft', async () => {
  for (const succeeds of [true, false]) {
    const { state, leaveGuard, navigations, navigationDirtyStates } = component('RegisteredServiceForm', {
      createService: async () => {
        assert.equal(leaveGuard.isDirty(), true)
        if (!succeeds) throw new Error('save failed')
        return { id: 42 }
      }
    })
    state.form.title = 'Draft'
    await state.handleSubmit()
    assert.equal(leaveGuard.isDirty(), !succeeds)
    assert.equal(navigations.length, succeeds ? 1 : 0)
    assert.deepEqual(navigationDirtyStates, succeeds ? [false] : [])
  }
})

test('switching editor identity reloads the target and ignores late responses from the previous service', async () => {
  let resolveOld
  const { state, definition, leaveGuard } = component('RegisteredServiceForm', {
    getService: id => id === 41 ? new Promise(resolve => { resolveOld = resolve }) : Promise.resolve({ id, service_name: 'current', title: 'Current', service_type: 'rest', endpoint_url: 'https://example.test' })
  })
  const switchRoute = definition.watch['$route.path'].handler.bind(state)
  state.$route = { path: '/services/41/edit', params: { id: '41' } }
  const old = switchRoute()
  state.$route = { path: '/services/42/edit', params: { id: '42' } }
  await switchRoute()
  resolveOld({ id: 41, service_name: 'old', title: 'Old' })
  await old
  assert.equal(state.form.title, 'Current')
  assert.equal(state.serviceId, 42)
  assert.equal(leaveGuard.isDirty(), false)
  state.$route = { path: '/services/create', params: {} }
  await switchRoute()
  assert.equal(state.isEdit, false)
  assert.equal(state.form.title, '')
  assert.equal(state.authConfig.token, '')
  assert.equal(leaveGuard.isDirty(), false)
})

for (const outcome of ['success', 'failure']) {
  test(`detail identity switch ignores a late ${outcome}, including A to B to A`, async () => {
    let resolveOld, rejectOld
    const { state, definition, messages, navigations } = component('RegisteredServiceDetail', {
      getService: () => new Promise((resolve, reject) => { resolveOld = resolve; rejectOld = reject })
    })
    const switchIdentity = definition.watch['$route.params.id'].handler.bind(state)
    const old = switchIdentity()
    const oldResolve = resolveOld
    const oldReject = rejectOld
    const finishOld = outcome === 'success' ? () => oldResolve({ id: 42, title: 'Old A' }) : () => oldReject(new Error('Old failure'))
    state.$route.params.id = '43'
    const next = switchIdentity()
    resolveOld({ id: 43, title: 'B' })
    await next
    state.$route.params.id = '42'
    const current = switchIdentity()
    assert.equal(state.service, null)
    assert.equal(state.loading, true)
    finishOld()
    await old
    assert.equal(state.service, null)
    assert.equal(state.loading, true)
    resolveOld({ id: 42, title: 'New A' })
    await current
    assert.equal(state.service.title, 'New A')
    assert.equal(state.loading, false)
    assert.deepEqual(messages, [])
    assert.deepEqual(navigations, [])
  })
}

test('detail overlapping reloads keep the newest response and ignore results after unmount', async () => {
  const pending = []
  const { state, definition, messages, navigations } = component('RegisteredServiceDetail', {
    getService: () => new Promise(resolve => pending.push(resolve))
  })
  const first = state.loadService()
  const second = state.loadService()
  pending[1]({ id: 42, title: 'Latest' })
  await second
  pending[0]({ id: 42, title: 'Old' })
  await first
  assert.equal(state.service.title, 'Latest')
  const third = state.loadService()
  definition.beforeUnmount.call(state)
  pending[2]({ id: 42, title: 'Unmounted' })
  await third
  assert.equal(state.service.title, 'Latest')
  assert.deepEqual(messages, [])
  assert.deepEqual(navigations, [])
})

for (const action of ['handleDelete', 'refreshMetadata']) {
  test(`${action}: changing identity during confirmation sends no mutation`, async () => {
    let approve
    const { state, definition, messages, navigations } = component('RegisteredServiceDetail', {
      getService: async id => ({ id, title: 'B' }),
      deleteService: () => assert.fail('stale delete'),
      refreshMetadata: () => assert.fail('stale refresh')
    }, () => new Promise(resolve => { approve = resolve }))
    state.service = { id: 42 }
    const pending = state[action]()
    state.$route.params.id = '43'
    await definition.watch['$route.params.id'].handler.call(state)
    approve()
    await pending
    assert.equal(state.service.id, '43')
    assert.deepEqual(messages, [])
    assert.deepEqual(navigations, [])
  })
}

for (const [action, api, flag] of [['handleDelete', 'deleteService', 'deleting'], ['refreshMetadata', 'refreshMetadata', 'refreshing'], ['healthCheck', 'healthCheck', 'checking']]) {
  for (const outcome of ['success', 'failure']) {
    test(`${action}: late ${outcome} does not notify, reload, navigate or unlock a new identity`, async () => {
      let finish
      let started
      const requestStarted = new Promise(resolve => { started = resolve })
      let loads = 0
      const { state, definition, messages, navigations } = component('RegisteredServiceDetail', {
        getService: async id => { loads++; return { id, title: 'B' } },
        [api]: id => new Promise((resolve, reject) => {
          assert.equal(id, 42)
          finish = () => outcome === 'success' ? resolve({ status: 'healthy' }) : reject(new Error('Old failure'))
          started()
        })
      })
      state.service = { id: 42 }
      const old = state[action]()
      await requestStarted
      state.$route.params.id = '43'
      await definition.watch['$route.params.id'].handler.call(state)
      state[flag] = true
      finish()
      await old
      assert.equal(state.service.id, '43')
      assert.equal(state[flag], true)
      assert.equal(loads, 1)
      assert.deepEqual(messages, [])
      assert.deepEqual(navigations, [])
    })
  }
}

for (const edit of [false, true]) {
  for (const outcome of ['success', 'failure']) {
    for (const transition of ['switch', 'unmount']) {
      test(`registration ${edit ? 'update' : 'create'} late ${outcome} after ${transition} leaves the new draft and navigation untouched`, async () => {
        let finish
        const save = () => new Promise((resolve, reject) => {
          finish = () => outcome === 'success' ? resolve({ id: 42 }) : reject(new Error('Old save failed'))
        })
        const { state, definition, leaveGuard, messages, navigations } = component('RegisteredServiceForm', {
          createService: save, updateService: save,
          getService: async id => ({ id, service_name: 'new_service', title: 'Loaded target', service_type: 'rest', endpoint_url: 'https://example.invalid/new' })
        })
        state.isEdit = edit
        state.serviceId = edit ? 42 : null
        state.$route = { path: edit ? '/services/42/edit' : '/services/create', params: edit ? { id: '42' } : {} }
        state.form.title = 'Submitted draft'
        const pending = state.handleSubmit()
        assert.equal(state.submitting, true)
        if (transition === 'switch') {
          state.$route = { path: '/services/43/edit', params: { id: '43' } }
          await definition.watch['$route.path'].handler.call(state)
          assert.equal(state.submitting, false, 'the new editor must be available')
        } else {
          definition.beforeUnmount?.call(state)
        }
        state.form.title = 'New unsaved draft'
        state.markSaved()
        assert.equal(leaveGuard.isDirty(), false)
        state.form.title = 'Newer unsaved draft'
        state.submitting = true
        finish()
        await pending
        assert.equal(state.form.title, 'Newer unsaved draft')
        assert.equal(leaveGuard.isDirty(), true)
        state.form.title = 'New unsaved draft'
        assert.equal(leaveGuard.isDirty(), false, 'the previous save must not replace the baseline')
        assert.equal(state.submitting, true, 'an old completion must not unlock a new submission')
        assert.deepEqual(messages, [])
        assert.deepEqual(navigations, [])
      })
    }
  }
}
