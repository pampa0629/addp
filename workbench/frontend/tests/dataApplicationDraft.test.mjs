import assert from 'node:assert/strict'
import test from 'node:test'
import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'
import { buildDataApplicationPreview, commitLatestDataApplicationRequest, confirmDataApplicationAction, dataApplicationEditorMutationContext, dataApplicationEditorRouteContext, normalizedApplicationSnapshot } from '../src/utils/dataApplicationDraft.mjs'

test('normalizes a Vue-style reactive snapshot without cloning the Proxy directly', () => {
  const snapshot = new Proxy({
    page: { id: 'page-a', title: 'Page', display_mode: 'desktop', refresh_interval_seconds: 0, visible_sections: ['title', 'parameters', 'query_actions'], placements: [] },
    components: [{ id: 'component-a' }],
    parameters: [
      { key: 'used', label: 'Used' },
      { key: 'unused', label: 'Unused' },
    ],
    parameter_bindings: [
      { application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'status' },
    ],
  }, {})

  assert.deepEqual(normalizedApplicationSnapshot(snapshot), {
    page: { id: 'page-a', title: 'Page', display_mode: 'desktop', refresh_interval_seconds: 0, visible_sections: ['title', 'parameters', 'query_actions'], placements: [] },
    components: [{ id: 'component-a' }],
    parameters: [{ key: 'used', label: 'Used' }],
    parameter_bindings: [
      { application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'status' },
    ],
  })
})

test('treats lifecycle dialog cancellation as a normal false result', async () => {
  assert.equal(await confirmDataApplicationAction(async () => { throw 'cancel' }, 'Publish?'), false)
  assert.equal(await confirmDataApplicationAction(async () => { throw 'close' }, 'Publish?'), false)
  await assert.rejects(() => confirmDataApplicationAction(async () => { throw new Error('broken dialog') }, 'Publish?'), /broken dialog/)
})

test('builds a detached preview from the same normalized payload as draft persistence', () => {
  const application = {
    name: '  Example application  ',
    description: '  Current draft  ',
    snapshot: {
      page: { title: 'Unsaved title' },
      components: [{ id: 'component-a' }],
      parameters: [{ key: 'used' }, { key: 'unused' }],
      parameter_bindings: [{ application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'filter' }],
    },
  }

  const preview = buildDataApplicationPreview(application)
  assert.deepEqual(preview, {
    name: 'Example application',
    description: 'Current draft',
    revision_number: 0,
    snapshot: {
      page: { title: 'Unsaved title' },
      components: [{ id: 'component-a' }],
      parameters: [{ key: 'used' }],
      parameter_bindings: [{ application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'filter' }],
    },
  })
  preview.snapshot.page.title = 'Preview interaction'
  assert.equal(application.snapshot.page.title, 'Unsaved title')
})

test('normalizes create and edit routes into one editor load context', () => {
  assert.equal(dataApplicationEditorRouteContext('DataApplicationCreate'), 'create')
  assert.equal(dataApplicationEditorRouteContext('DataApplicationEdit', '  application-a  '), 'edit:application-a')
})

test('keeps application B when application A responds after the editor route changes', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { application: null }
  let routeName = 'DataApplicationEdit'
  let applicationID = 'application-a'
  const requestA = requests.begin(dataApplicationEditorRouteContext(routeName, applicationID))
  let releaseA
  const responseA = new Promise((resolve) => { releaseA = resolve })
  const loadA = responseA.then((application) => commitLatestDataApplicationRequest(
    requests, requestA, dataApplicationEditorRouteContext(routeName, applicationID), () => { state.application = application },
  ))

  applicationID = 'application-b'
  const requestB = requests.begin(dataApplicationEditorRouteContext(routeName, applicationID))
  assert.equal(commitLatestDataApplicationRequest(
    requests, requestB, dataApplicationEditorRouteContext(routeName, applicationID), () => { state.application = { id: applicationID } },
  ), true)

  releaseA({ id: 'application-a' })
  assert.equal(await loadA, false)
  assert.deepEqual(state.application, { id: 'application-b' })
})

test('rejects an old application descriptor after the next editor route is loaded', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { application: null, descriptors: {} }
  const routeName = 'DataApplicationEdit'
  let applicationID = 'application-a'
  const requestA = requests.begin(dataApplicationEditorRouteContext(routeName, applicationID))
  assert.equal(commitLatestDataApplicationRequest(
    requests, requestA, dataApplicationEditorRouteContext(routeName, applicationID), () => { state.application = { id: applicationID } },
  ), true)
  let releaseDescriptorA
  const descriptorA = new Promise((resolve) => { releaseDescriptorA = resolve })
  const loadDescriptorA = descriptorA.then((descriptor) => commitLatestDataApplicationRequest(
    requests, requestA, dataApplicationEditorRouteContext(routeName, applicationID), () => { state.descriptors.a = descriptor },
  ))

  applicationID = 'application-b'
  const requestB = requests.begin(dataApplicationEditorRouteContext(routeName, applicationID))
  commitLatestDataApplicationRequest(requests, requestB, dataApplicationEditorRouteContext(routeName, applicationID), () => {
    state.application = { id: applicationID }
    state.descriptors = { b: { contract_fingerprint: 'current' } }
  })

  releaseDescriptorA({ contract_fingerprint: 'obsolete' })
  assert.equal(await loadDescriptorA, false)
  assert.deepEqual(state, {
    application: { id: 'application-b' },
    descriptors: { b: { contract_fingerprint: 'current' } },
  })
})

test('rejects editor load commits after the page is unmounted', async () => {
  const requests = createLatestRequestCoordinator()
  const routeName = 'DataApplicationEdit'
  const applicationID = 'application-a'
  const request = requests.begin(dataApplicationEditorRouteContext(routeName, applicationID))
  let releaseResponse
  const response = new Promise((resolve) => { releaseResponse = resolve })
  let committed = false
  const load = response.then(() => commitLatestDataApplicationRequest(
    requests, request, dataApplicationEditorRouteContext(routeName, applicationID), () => { committed = true },
  ))

  requests.invalidate()
  releaseResponse()

  assert.equal(await load, false)
  assert.equal(committed, false)
})

test('keeps application B saving when application A save settles after a route switch', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { application: null, saving: false }
  const routeName = 'DataApplicationEdit'
  let applicationID = 'application-a'
  const requestA = requests.begin(dataApplicationEditorMutationContext(routeName, applicationID, 'save'))
  state.saving = true
  let releaseA
  const responseA = new Promise((resolve) => { releaseA = resolve })
  let finallyACommitted
  const saveA = responseA
    .then((application) => commitLatestDataApplicationRequest(
      requests,
      requestA,
      dataApplicationEditorMutationContext(routeName, applicationID, 'save'),
      () => { state.application = application },
    ))
    .finally(() => {
      finallyACommitted = commitLatestDataApplicationRequest(
        requests,
        requestA,
        dataApplicationEditorMutationContext(routeName, applicationID, 'save'),
        () => { state.saving = false },
      )
    })

  applicationID = 'application-b'
  const requestB = requests.begin(dataApplicationEditorMutationContext(routeName, applicationID, 'save'))
  state.saving = true
  commitLatestDataApplicationRequest(
    requests,
    requestB,
    dataApplicationEditorMutationContext(routeName, applicationID, 'save'),
    () => { state.application = { id: applicationID } },
  )

  releaseA({ id: 'application-a' })
  assert.equal(await saveA, false)
  assert.equal(finallyACommitted, false)
  assert.deepEqual(state, { application: { id: 'application-b' }, saving: true })
})

test('rejects a confirmed mutation when its editor route changed while confirmation was open', async () => {
  const requests = createLatestRequestCoordinator()
  const routeName = 'DataApplicationEdit'
  let applicationID = 'application-a'
  const request = requests.begin(dataApplicationEditorMutationContext(routeName, applicationID, 'publish'))
  let releaseConfirmation
  const confirmation = new Promise((resolve) => { releaseConfirmation = resolve })
  let mutationStarted = false
  const publish = confirmation.then(() => commitLatestDataApplicationRequest(
    requests,
    request,
    dataApplicationEditorMutationContext(routeName, applicationID, 'publish'),
    () => { mutationStarted = true },
  ))

  applicationID = 'application-b'
  releaseConfirmation(true)

  assert.equal(await publish, false)
  assert.equal(mutationStarted, false)
})

test('does not remove a component from the next application when confirmation settles late', async () => {
  const requests = createLatestRequestCoordinator()
  const routeName = 'DataApplicationEdit'
  let applicationID = 'application-a'
  const action = 'remove-component:component-a'
  const request = requests.begin(dataApplicationEditorMutationContext(routeName, applicationID, action))
  let releaseConfirmation
  const confirmation = new Promise((resolve) => { releaseConfirmation = resolve })
  const state = { components: ['component-a', 'component-b'] }
  const removal = confirmation.then(() => commitLatestDataApplicationRequest(
    requests,
    request,
    dataApplicationEditorMutationContext(routeName, applicationID, action),
    () => { state.components = state.components.filter((id) => id !== 'component-a') },
  ))

  applicationID = 'application-b'
  releaseConfirmation(true)

  assert.equal(await removal, false)
  assert.deepEqual(state.components, ['component-a', 'component-b'])
})
