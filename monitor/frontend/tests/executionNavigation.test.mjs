import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import { executionDetailLocation } from '../src/utils/executionNavigation.js'

test('builds the execution detail route from the stable execution UUID', () => {
  assert.deepEqual(executionDetailLocation({
    id: 1582,
    execution_id: '26ec7f65-4a2f-4654-a375-5428dba6dd2c'
  }), {
    path: '/executions',
    query: {
      execution_id: '26ec7f65-4a2f-4654-a375-5428dba6dd2c'
    }
  })
})

test('does not fall back to the database row id', () => {
  assert.equal(executionDetailLocation({ id: 1582 }), null)
})

test('dashboard delegates recent execution details to the execution route', () => {
  const dashboardSource = readFileSync(
    new URL('../src/views/Dashboard.vue', import.meta.url),
    'utf8'
  )

  assert.match(dashboardSource, /const location = executionDetailLocation\(row\)/)
  assert.match(dashboardSource, /navigateMonitorRoute\(router, location\)/)
  assert.doesNotMatch(dashboardSource, /monitor\.dashboard\.view_execution/)
})

test('execution list and stable tabs delegate to Monitor module navigation', () => {
  const executionListSource = readFileSync(
    new URL('../src/views/ExecutionList.vue', import.meta.url),
    'utf8'
  )
  const alertSource = readFileSync(new URL('../src/views/AlertList.vue', import.meta.url), 'utf8')
  const notificationSource = readFileSync(new URL('../src/views/NotificationList.vue', import.meta.url), 'utf8')
  const navigationSource = readFileSync(new URL('../src/utils/moduleNavigation.js', import.meta.url), 'utf8')

  assert.match(navigationSource, /navigateConsoleModuleRoute\(router, 'monitor', location, options\)/)
  assert.match(executionListSource, /const location = executionDetailLocation\(row\)/)
  assert.match(executionListSource, /navigateMonitorRoute\(router, location\)/)
  assert.match(executionListSource, /execution_id: undefined/)
  assert.match(alertSource, /resolveMonitorTabRouteState/)
  assert.match(notificationSource, /resolveMonitorTabRouteState/)
  assert.match(alertSource, /watch\(\(\) => route\.query, restoreTabFromRoute\)/)
  assert.match(notificationSource, /watch\(\(\) => route\.query, restoreTabFromRoute\)/)
})

test('direct execution links reveal an explicit detail loading state without waiting for the list', () => {
  const executionListSource = readFileSync(
    new URL('../src/views/ExecutionList.vue', import.meta.url),
    'utf8'
  )

  assert.match(executionListSource, /const detailLoading = ref\(false\)/)
  assert.match(executionListSource, /v-if="detailLoading" class="detail-loading"/)
  assert.match(executionListSource, /<StatusAnnouncer/)
  assert.match(executionListSource, /detailDialogVisible\.value = true[\s\S]*?detailLoading\.value = true/)
  assert.match(executionListSource, /Promise\.all\(\[taskProvidersPromise, executionsPromise, detailPromise\]\)/)
})

test('running list rows and the opened detail refresh independently', () => {
  const executionListSource = readFileSync(
    new URL('../src/views/ExecutionList.vue', import.meta.url),
    'utf8'
  )

  assert.match(executionListSource, /const EXECUTION_LIST_REFRESH_INTERVAL_MS = 5000/)
  assert.match(executionListSource, /const EXECUTION_DETAIL_REFRESH_INTERVAL_MS = 1000/)
  assert.match(executionListSource, /const hasRunningListExecution = computed/)
  assert.match(executionListSource, /const hasRunningOpenedExecution = computed/)
  assert.match(executionListSource, /window\.setInterval\(refreshRunningExecutionList, EXECUTION_LIST_REFRESH_INTERVAL_MS\)/)
  assert.match(executionListSource, /window\.setInterval\(refreshOpenedExecution, EXECUTION_DETAIL_REFRESH_INTERVAL_MS\)/)
  assert.match(executionListSource, /executionListRefreshInFlight/)
  assert.match(executionListSource, /executionDetailRefreshInFlight/)
  assert.match(executionListSource, /openedExecutionID\.value !== executionID/)
  assert.match(executionListSource, /watch\(shouldRefreshExecutionList/)
  assert.match(executionListSource, /watch\(shouldRefreshOpenedExecution/)
  assert.doesNotMatch(executionListSource, /async function refreshRunningExecutions/)
})

test('execution tree refresh preserves the selected child by execution UUID', () => {
  const executionListSource = readFileSync(
    new URL('../src/views/ExecutionList.vue', import.meta.url),
    'utf8'
  )

  assert.match(executionListSource, /:current-node-key="currentExecution\?\.execution_id"/)
  assert.match(executionListSource, /const selectedExecutionID = currentExecution\.value\?\.execution_id/)
  assert.match(executionListSource, /findExecutionTreeNodeByID\(tree, selectedExecutionID\)\?\.execution \|\| tree\.execution/)
  assert.match(executionListSource, /function findExecutionTreeNodeByID\(node, executionID\)/)
})

test('automatic refresh pauses while the page is hidden and catches up when visible', () => {
  const executionListSource = readFileSync(
    new URL('../src/views/ExecutionList.vue', import.meta.url),
    'utf8'
  )

  assert.match(executionListSource, /const pageVisible = ref\(!document\.hidden\)/)
  assert.match(executionListSource, /const shouldRefreshExecutionList = computed/)
  assert.match(executionListSource, /const shouldRefreshOpenedExecution = computed/)
  assert.match(executionListSource, /if \(!shouldRefreshExecutionList\.value \|\| executionListRefreshInFlight\)/)
  assert.match(executionListSource, /if \(!shouldRefreshOpenedExecution\.value \|\| !hasValue\(openedExecutionID\.value\)/)
  assert.match(executionListSource, /function handleVisibilityChange\(\)/)
  assert.match(executionListSource, /document\.addEventListener\('visibilitychange', handleVisibilityChange\)/)
  assert.match(executionListSource, /document\.removeEventListener\('visibilitychange', handleVisibilityChange\)/)
  assert.match(executionListSource, /void refreshRunningExecutionList\(\)/)
  assert.match(executionListSource, /void refreshOpenedExecution\(\)/)
})
