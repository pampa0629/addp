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
