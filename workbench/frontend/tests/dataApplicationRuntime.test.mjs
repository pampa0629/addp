import assert from 'node:assert/strict'
import test from 'node:test'
import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'
import { applicationRefreshDelayMilliseconds, buildComponentQuery, buildSelectionUpdate, canAttemptApplicationQuery, canExecuteComponentQuery, canHideApplicationParameters, canRunApplicationRefresh, commitLatestComponentDescriptorState, commitLatestDataApplicationLoad, componentBlockingError, componentIDsForApplicationParameters, initialApplicationParameterValues, invalidateApplicationParameterResults, runtimeGridStyle, runtimeLayoutStyle, runtimeSectionVisible } from '../src/utils/dataApplicationRuntime.mjs'
import { downloadCurrentBoundedExport } from '../src/utils/boundedExport.mjs'

const component = {
  id: 'component-a',
  query_template: {
    select: ['id', 'amount'],
    fixed_filter: { field: 'active', op: 'eq', value: true },
    parameter_filters: [
      { parameter_key: 'minimum', field: 'amount', operator: 'gte' },
      { parameter_key: 'missing', field: 'deleted_at', operator: 'is_null' },
    ],
    order_by: [{ field: 'id', direction: 'asc' }],
    page_limit: 50,
    format: 'json',
  },
}

const snapshot = {
  parameters: [
    { key: 'minimum_amount', label: 'Minimum', control_type: 'number', required: true, default_value: 10 },
    { key: 'missing_rows', label: 'Missing', control_type: 'checkbox', required: false, default_value: false },
  ],
  parameter_bindings: [
    { application_parameter_key: 'minimum_amount', component_id: 'component-a', component_parameter_key: 'minimum' },
    { application_parameter_key: 'missing_rows', component_id: 'component-a', component_parameter_key: 'missing' },
  ],
  selection_bindings: [{
    source_component_id: 'component-source',
    assignments: [{ source_field: 'amount', application_parameter_key: 'minimum_amount' }],
  }],
}

test('builds a component query only from explicit application parameter bindings', () => {
  const values = initialApplicationParameterValues(snapshot)
  assert.deepEqual(values, { minimum_amount: 10, missing_rows: false })
  assert.deepEqual(buildComponentQuery(snapshot, component, values, 'next-page-cursor'), {
    parameters: {},
    select: ['id', 'amount'],
    filter: { and: [
      { field: 'active', op: 'eq', value: true },
      { field: 'amount', op: 'gte', value: 10 },
    ] },
    order_by: [{ field: 'id', direction: 'asc' }],
    page: { limit: 50, cursor: 'next-page-cursor' },
    format: 'json',
  })
  assert.equal(buildComponentQuery(snapshot, component, values, '', 'csv').format, 'csv')
})

test('binds application parameters to service named parameters without creating field filters', () => {
  const namedComponent = structuredClone(component)
  namedComponent.query_template.parameter_filters = []
  namedComponent.query_template.named_parameter_bindings = [
    { parameter_key: 'first-person', name: 'person_id_a' },
    { parameter_key: 'second-person', name: 'person_id_b' },
  ]
  const namedSnapshot = structuredClone(snapshot)
  namedSnapshot.parameters = [
    { key: 'person-a', required: true },
    { key: 'person-b', required: true },
  ]
  namedSnapshot.parameter_bindings = [
    { application_parameter_key: 'person-a', component_id: namedComponent.id, component_parameter_key: 'first-person' },
    { application_parameter_key: 'person-b', component_id: namedComponent.id, component_parameter_key: 'second-person' },
  ]
  const query = buildComponentQuery(namedSnapshot, namedComponent, { 'person-a': 'a', 'person-b': 'b' })
  assert.deepEqual(query.parameters, { person_id_a: 'a', person_id_b: 'b' })
  assert.deepEqual(query.filter, { field: 'active', op: 'eq', value: true })
})

test('maps a result selection atomically to application parameters and deduplicated component targets', () => {
  const linkedSnapshot = structuredClone(snapshot)
  linkedSnapshot.parameter_bindings.push({ application_parameter_key: 'minimum_amount', component_id: 'component-b', component_parameter_key: 'minimum' })
  linkedSnapshot.parameter_bindings.push({ application_parameter_key: 'minimum_amount', component_id: 'component-a', component_parameter_key: 'other-minimum' })
  assert.deepEqual(buildSelectionUpdate(
    linkedSnapshot,
    'component-source',
    { output_contract: { fields: [{ name: 'amount', type: 'decimal' }] } },
    [{ amount: 12.5 }],
    { row_index: 0 },
  ), {
    parameter_values: { minimum_amount: 12.5 },
    component_ids: ['component-a', 'component-b'],
  })
})

test('derives only the components affected by changed application parameters', () => {
  const linkedSnapshot = structuredClone(snapshot)
  linkedSnapshot.parameter_bindings.push({ application_parameter_key: 'minimum_amount', component_id: 'component-b', component_parameter_key: 'minimum' })
  linkedSnapshot.parameter_bindings.push({ application_parameter_key: 'minimum_amount', component_id: 'component-a', component_parameter_key: 'other-minimum' })

  assert.deepEqual(componentIDsForApplicationParameters(linkedSnapshot, ['minimum_amount']), ['component-a', 'component-b'])
  assert.deepEqual(componentIDsForApplicationParameters(linkedSnapshot, ['missing_rows']), ['component-a'])
  assert.deepEqual(componentIDsForApplicationParameters(linkedSnapshot, ['unknown']), [])
})

test('invalidates an in-flight request before clearing results for a changed parameter', async () => {
  const requests = createLatestRequestCoordinator()
  const request = requests.begin('component-a')
  let releaseOldResponse
  const oldResponse = new Promise((resolve) => { releaseOldResponse = resolve })
  const current = {
    requests,
    querying: true,
    exporting: true,
    query_error: 'old error',
    query_completed: true,
    rows: [{ city: 'old' }],
    page: { has_more: true, next_cursor: 'old-cursor' },
    cursors: ['', 'old-cursor'],
    cursor_index: 1,
  }
  const unaffected = {
    requests: createLatestRequestCoordinator(),
    querying: false,
    exporting: false,
    query_error: '',
    query_completed: true,
    rows: [{ city: 'unaffected' }],
    page: { has_more: false, next_cursor: '' },
    cursors: [''],
    cursor_index: 0,
  }
  const states = { 'component-a': current, 'component-b': unaffected }
  let committed = false
  const pendingCommit = oldResponse.then((rows) => {
    if (!requests.isCurrent(request, 'component-a')) return
    current.rows = rows
    current.querying = false
    committed = true
  })

  assert.deepEqual(invalidateApplicationParameterResults(snapshot, states, ['minimum_amount']), ['component-a'])
  assert.deepEqual(current.rows, [])
  assert.deepEqual(current.page, { has_more: false, next_cursor: '' })
  assert.deepEqual(current.cursors, [''])
  assert.equal(current.cursor_index, 0)
  assert.equal(current.query_error, '')
  assert.equal(current.query_completed, false)
  assert.equal(current.querying, false)
  assert.equal(current.exporting, false)
  assert.deepEqual(unaffected.rows, [{ city: 'unaffected' }])
  assert.equal(unaffected.query_completed, true)

  releaseOldResponse([{ city: 'late-old' }])
  await pendingCommit
  assert.equal(committed, false)
  assert.deepEqual(current.rows, [])
  assert.equal(current.querying, false)
})

test('does not download an export response that arrives after its parameter context changed', async () => {
  const requests = createLatestRequestCoordinator()
  const request = requests.begin('component-a')
  let releaseOldExport
  const oldExport = new Promise((resolve) => { releaseOldExport = resolve })
  const current = {
    requests,
    querying: false,
    exporting: true,
    query_error: '',
    query_completed: true,
    rows: [{ id: 1 }],
    page: { has_more: false, next_cursor: '' },
    cursors: [''],
    cursor_index: 0,
  }
  const pendingDownload = oldExport.then((response) => downloadCurrentBoundedExport(
    response,
    'old-result.csv',
    () => requests.isCurrent(request, 'component-a'),
  ))

  invalidateApplicationParameterResults(snapshot, { 'component-a': current }, ['minimum_amount'])
  releaseOldExport({ data: 'id\n1\n', headers: {} })

  assert.equal(await pendingDownload, 'stale')
  assert.equal(current.exporting, false)
})

test('keeps a newer descriptor success when an older descriptor failure arrives later', async () => {
  const descriptorRequests = createLatestRequestCoordinator()
  const current = { descriptor: null, descriptor_error: '', contract_error: '', descriptorRequests }
  const oldRequest = descriptorRequests.begin('component-a')
  let releaseOldFailure
  const oldFailure = new Promise((resolve) => { releaseOldFailure = resolve })
  const oldCommit = oldFailure.then(() => commitLatestComponentDescriptorState(
    current,
    oldRequest,
    'component-a',
    { descriptor: null, descriptor_error: 'old failure', contract_error: '' },
  ))

  const newRequest = descriptorRequests.begin('component-a')
  let releaseNewSuccess
  const newSuccess = new Promise((resolve) => { releaseNewSuccess = resolve })
  const newDescriptor = { contract_fingerprint: 'current-fingerprint', operations: [] }
  const newCommit = newSuccess.then(() => commitLatestComponentDescriptorState(
    current,
    newRequest,
    'component-a',
    { descriptor: newDescriptor, descriptor_error: '', contract_error: '' },
  ))

  releaseNewSuccess()
  assert.equal(await newCommit, true)
  releaseOldFailure()
  assert.equal(await oldCommit, false)
  assert.equal(current.descriptor, newDescriptor)
  assert.equal(current.descriptor_error, '')
  assert.equal(current.contract_error, '')
})

test('keeps route B loading and data when route A settles after the route switch', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { application: null, pageError: '', loading: false }
  let currentApplicationID = 'application-a'
  const requestA = requests.begin(currentApplicationID)
  state.loading = true
  let releaseA
  const responseA = new Promise((resolve) => { releaseA = resolve })
  const loadA = responseA
    .then((data) => commitLatestDataApplicationLoad(requests, requestA, currentApplicationID, () => { state.application = data }))
    .finally(() => commitLatestDataApplicationLoad(requests, requestA, currentApplicationID, () => { state.loading = false }))

  currentApplicationID = 'application-b'
  const requestB = requests.begin(currentApplicationID)
  state.application = null
  state.pageError = ''
  state.loading = true
  let releaseB
  const responseB = new Promise((resolve) => { releaseB = resolve })
  const loadB = responseB
    .then((data) => commitLatestDataApplicationLoad(requests, requestB, currentApplicationID, () => { state.application = data }))
    .finally(() => commitLatestDataApplicationLoad(requests, requestB, currentApplicationID, () => { state.loading = false }))

  releaseA({ id: 'application-a' })
  assert.equal(await loadA, false)
  assert.equal(state.application, null)
  assert.equal(state.loading, true)

  releaseB({ id: 'application-b' })
  assert.equal(await loadB, true)
  assert.deepEqual(state.application, { id: 'application-b' })
  assert.equal(state.pageError, '')
  assert.equal(state.loading, false)
})

test('ignores application load commits after the runtime page is unmounted', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { application: null, pageError: '', loading: true }
  const currentApplicationID = 'application-a'
  const request = requests.begin(currentApplicationID)
  let releaseResponse
  const response = new Promise((resolve) => { releaseResponse = resolve })
  let successCommitted
  let finallyCommitted
  const load = response
    .then((data) => {
      successCommitted = commitLatestDataApplicationLoad(requests, request, currentApplicationID, () => {
        state.application = data
      })
    })
    .finally(() => {
      finallyCommitted = commitLatestDataApplicationLoad(requests, request, currentApplicationID, () => {
        state.loading = false
      })
    })

  requests.invalidate()
  releaseResponse({ id: currentApplicationID })
  await load

  assert.equal(successCommitted, false)
  assert.equal(finallyCommitted, false)
  assert.deepEqual(state, { application: null, pageError: '', loading: true })
})

test('ignores components without a selection binding and rejects invalid selected values', () => {
  assert.equal(buildSelectionUpdate(snapshot, 'component-a', {}, [], { row_index: 0 }), null)
  assert.throws(() => buildSelectionUpdate(
    snapshot,
    'component-source',
    { output_contract: { fields: [{ name: 'amount', type: 'decimal' }] } },
    [{ amount: '12.5' }],
    { row_index: 0 },
  ), /invalid selection value/)
})

test('rejects a missing required application parameter and maps the twelve-column layout', () => {
  assert.throws(() => buildComponentQuery(snapshot, component, { minimum_amount: '' }), /missing required application parameter/)
  assert.deepEqual(runtimeLayoutStyle({ x: 2, y: 4, width: 6, height: 5 }), {
    gridColumn: '3 / span 6',
    gridRow: '5 / span 5',
  })
})

test('fits every wallboard placement row into the current runtime grid', () => {
  const placements = [
    { x: 0, y: 0, width: 6, height: 4 },
    { x: 6, y: 0, width: 6, height: 4 },
    { x: 0, y: 4, width: 12, height: 3 },
  ]
  assert.deepEqual(runtimeGridStyle({ display_mode: 'desktop', placements }), {})
  assert.deepEqual(runtimeGridStyle({ display_mode: 'wallboard', placements }), {
    gridTemplateRows: 'repeat(7, minmax(0, 1fr))',
  })
})

test('runs only supported wallboard refresh intervals while visible and idle', () => {
  const wallboard = { display_mode: 'wallboard', refresh_interval_seconds: 60 }
  assert.equal(applicationRefreshDelayMilliseconds(wallboard), 60_000)
  assert.equal(canRunApplicationRefresh(wallboard), true)
  assert.equal(canRunApplicationRefresh(wallboard, { hidden: true }), false)
  assert.equal(canRunApplicationRefresh(wallboard, { querying: true }), false)
  assert.equal(applicationRefreshDelayMilliseconds({ display_mode: 'desktop', refresh_interval_seconds: 60 }), 0)
  assert.equal(applicationRefreshDelayMilliseconds({ display_mode: 'wallboard', refresh_interval_seconds: 10 }), 0)
  assert.equal(applicationRefreshDelayMilliseconds({ display_mode: 'wallboard', refresh_interval_seconds: 0 }), 0)
})

test('keeps transient descriptor and query failures retryable while contract drift stays blocked', () => {
  const components = [{ id: 'component-a' }, { id: 'component-b' }]
  const states = {
    'component-a': { descriptor: null, descriptor_error: 'temporarily unavailable', contract_error: '', query_error: '' },
    'component-b': { descriptor: { operations: [] }, descriptor_error: '', contract_error: '', query_error: 'query failed' },
  }

  assert.equal(canAttemptApplicationQuery(components, states), true)
  assert.equal(canExecuteComponentQuery(states['component-a']), false)
  assert.equal(canExecuteComponentQuery(states['component-b']), true)
  assert.equal(componentBlockingError(states['component-a']), 'temporarily unavailable')
  assert.equal(componentBlockingError(states['component-b']), '')

  states['component-a'].contract_error = 'contract changed'
  states['component-a'].descriptor_error = ''
  assert.equal(canAttemptApplicationQuery(components, states), false)
  assert.equal(canExecuteComponentQuery(states['component-a']), false)
  assert.equal(componentBlockingError(states['component-a']), 'contract changed')
})

test('uses explicit runtime sections and only hides required parameters with executable defaults', () => {
  const page = { visible_sections: ['title', 'query_actions'] }
  assert.equal(runtimeSectionVisible(page, 'title'), true)
  assert.equal(runtimeSectionVisible(page, 'parameters'), false)
  assert.equal(runtimeSectionVisible({}, 'title'), false)
  const presentationSnapshot = structuredClone(snapshot)
  presentationSnapshot.components = [component]
  assert.equal(canHideApplicationParameters(presentationSnapshot), true)
  const missingDefault = structuredClone(presentationSnapshot)
  delete missingDefault.parameters[0].default_value
  assert.equal(canHideApplicationParameters(missingDefault), false)
  const nullOperatorDefault = structuredClone(presentationSnapshot)
  nullOperatorDefault.parameters[1].required = true
  assert.equal(canHideApplicationParameters(nullOperatorDefault), false)
  nullOperatorDefault.parameters[1].default_value = true
  assert.equal(canHideApplicationParameters(nullOperatorDefault), true)
})
