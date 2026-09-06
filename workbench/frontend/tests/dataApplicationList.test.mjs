import assert from 'node:assert/strict'
import test from 'node:test'
import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'
import { commitLatestDataApplicationRequest, dataApplicationDeletionContext, dataApplicationListPageContext } from '../src/utils/dataApplicationDraft.mjs'

test('normalizes list pages and deletion versions into stable request contexts', () => {
  assert.equal(dataApplicationListPageContext(2), 'page:2')
  assert.equal(dataApplicationListPageContext(' 3 '), 'page:3')
  assert.equal(dataApplicationListPageContext(0), 'page:1')
  assert.equal(dataApplicationListPageContext('invalid'), 'page:1')
  assert.equal(dataApplicationDeletionContext(' application-a ', 4), 'delete:application-a:4')
})

test('keeps the newest page when an older list request settles late', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { page: 1, items: [], loading: true }
  const requestA = requests.begin(dataApplicationListPageContext(state.page))
  let releaseA
  const responseA = new Promise((resolve) => { releaseA = resolve })
  const loadA = responseA.then((items) => commitLatestDataApplicationRequest(
    requests,
    requestA,
    dataApplicationListPageContext(state.page),
    () => { state.items = items; state.loading = false },
  ))

  state.page = 2
  const requestB = requests.begin(dataApplicationListPageContext(state.page))
  commitLatestDataApplicationRequest(requests, requestB, dataApplicationListPageContext(state.page), () => {
    state.items = ['page-b']
    state.loading = false
  })
  releaseA(['page-a'])

  assert.equal(await loadA, false)
  assert.deepEqual(state, { page: 2, items: ['page-b'], loading: false })
})

test('rejects list commits after the page is unmounted', async () => {
  const requests = createLatestRequestCoordinator()
  const page = 1
  const request = requests.begin(dataApplicationListPageContext(page))
  let release
  const response = new Promise((resolve) => { release = resolve })
  let committed = false
  const load = response.then(() => commitLatestDataApplicationRequest(
    requests,
    request,
    dataApplicationListPageContext(page),
    () => { committed = true },
  ))

  requests.invalidate()
  release()

  assert.equal(await load, false)
  assert.equal(committed, false)
})

test('does not start deletion when its confirmation settles after unmount', async () => {
  const requests = createLatestRequestCoordinator()
  const targetContext = dataApplicationDeletionContext('application-a', 1)
  const request = requests.begin(targetContext)
  let releaseConfirmation
  const confirmation = new Promise((resolve) => { releaseConfirmation = resolve })
  let deletionStarted = false
  const deletion = confirmation.then(() => commitLatestDataApplicationRequest(
    requests,
    request,
    targetContext,
    () => { deletionStarted = true },
  ))

  requests.invalidate()
  releaseConfirmation(true)

  assert.equal(await deletion, false)
  assert.equal(deletionStarted, false)
})

test('does not commit a deletion response after the list is unmounted', async () => {
  const requests = createLatestRequestCoordinator()
  const targetContext = dataApplicationDeletionContext('application-a', 1)
  const request = requests.begin(targetContext)
  let releaseResponse
  const response = new Promise((resolve) => { releaseResponse = resolve })
  const state = { messageShown: false, refreshed: false, deletingID: 'application-a' }
  const deletion = response.then(() => commitLatestDataApplicationRequest(
    requests,
    request,
    targetContext,
    () => { state.messageShown = true; state.refreshed = true; state.deletingID = '' },
  ))

  requests.invalidate()
  releaseResponse()

  assert.equal(await deletion, false)
  assert.deepEqual(state, { messageShown: false, refreshed: false, deletingID: 'application-a' })
})
