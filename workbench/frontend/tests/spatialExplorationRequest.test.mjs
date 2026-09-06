import assert from 'node:assert/strict'
import test from 'node:test'
import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'
import { commitLatestDataApplicationRequest } from '../src/utils/dataApplicationDraft.mjs'

test('keeps the newest descriptor when a previous service selection settles late', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { key: 'query:1', descriptor: null, loading: true }
  const requestA = requests.begin(state.key)
  let releaseA
  const responseA = new Promise((resolve) => { releaseA = resolve })
  const loadA = responseA.then((descriptor) => commitLatestDataApplicationRequest(
    requests,
    requestA,
    state.key,
    () => { state.descriptor = descriptor; state.loading = false },
  ))

  state.key = 'query:2'
  const requestB = requests.begin(state.key)
  commitLatestDataApplicationRequest(requests, requestB, state.key, () => {
    state.descriptor = { id: 2 }
    state.loading = false
  })
  releaseA({ id: 1 })

  assert.equal(await loadA, false)
  assert.deepEqual(state, { key: 'query:2', descriptor: { id: 2 }, loading: false })
})

test('keeps spatial loading active when the aggregate request finishes first', () => {
  const aggregateRequests = createLatestRequestCoordinator()
  const spatialRequests = createLatestRequestCoordinator()
  const state = { aggregateLoading: true, spatialLoading: true }
  const aggregate = aggregateRequests.begin('query:aggregate')
  const spatial = spatialRequests.begin('query:spatial')

  commitLatestDataApplicationRequest(aggregateRequests, aggregate, 'query:aggregate', () => { state.aggregateLoading = false })

  assert.equal(state.aggregateLoading || state.spatialLoading, true)
  assert.equal(spatialRequests.isCurrent(spatial, 'query:spatial'), true)
})

test('rejects catalog and descriptor commits after the wizard closes', async () => {
  const catalogRequests = createLatestRequestCoordinator()
  const descriptorRequests = createLatestRequestCoordinator()
  const catalog = catalogRequests.begin('catalog')
  const descriptor = descriptorRequests.begin('query:1')
  let release
  const response = new Promise((resolve) => { release = resolve })
  const state = { services: [], descriptor: null }
  const requests = response.then(({ services, selected }) => [
    commitLatestDataApplicationRequest(catalogRequests, catalog, 'catalog', () => { state.services = services }),
    commitLatestDataApplicationRequest(descriptorRequests, descriptor, 'query:1', () => { state.descriptor = selected }),
  ])

  catalogRequests.invalidate()
  descriptorRequests.invalidate()
  release({ services: [{ id: 1 }], selected: { id: 1 } })

  assert.deepEqual(await requests, [false, false])
  assert.deepEqual(state, { services: [], descriptor: null })
})

test('does not let a previous catalog session overwrite a reopened wizard', async () => {
  const requests = createLatestRequestCoordinator()
  const state = { services: [] }
  const requestA = requests.begin('catalog')
  let releaseA
  const responseA = new Promise((resolve) => { releaseA = resolve })
  const loadA = responseA.then((services) => commitLatestDataApplicationRequest(
    requests,
    requestA,
    'catalog',
    () => { state.services = services },
  ))

  requests.invalidate()
  const requestB = requests.begin('catalog')
  commitLatestDataApplicationRequest(requests, requestB, 'catalog', () => { state.services = [{ id: 2 }] })
  releaseA([{ id: 1 }])

  assert.equal(await loadA, false)
  assert.deepEqual(state.services, [{ id: 2 }])
})
