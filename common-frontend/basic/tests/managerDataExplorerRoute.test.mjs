import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

import { buildManagerDataExplorerRoute } from '../src/utils/managerDataExplorerRoute.js'

const repositoryRoot = resolve(import.meta.dirname, '../../..')
const source = relativePath => readFileSync(resolve(repositoryRoot, relativePath), 'utf8')

test('builds the single Console route for a Manager Data Explorer resource', () => {
  const locator = 'addp://engine/11/path/public/farmland?type=table&item_id=88'

  assert.equal(
    buildManagerDataExplorerRoute(locator),
    '/manager/data-explorer?locator=addp%3A%2F%2Fengine%2F11%2Fpath%2Fpublic%2Ffarmland%3Ftype%3Dtable%26item_id%3D88'
  )
})

test('opens the Manager Data Explorer root when no resource is provided', () => {
  assert.equal(buildManagerDataExplorerRoute('  '), '/manager/data-explorer')
})

test('keeps cross-module Manager Data Explorer route construction in one shared owner', () => {
  const sharedRoute = source('common-frontend/basic/src/utils/managerDataExplorerRoute.js')
  const managerRouteState = source('manager/frontend/src/utils/dataExplorerRoute.js')
  const monitorLineage = source('monitor/frontend/src/components/ExecutionLineageSummary.vue')

  assert.match(sharedRoute, /\/manager\/data-explorer/)
  assert.doesNotMatch(managerRouteState, /\/manager\/data-explorer/)
  assert.doesNotMatch(managerRouteState, /buildDataExplorerConsoleRoute/)
  assert.match(monitorLineage, /buildManagerDataExplorerRoute\(resource\.locator\)/)
  assert.doesNotMatch(monitorLineage, /['"]\/manager\/data-explorer/)
})
