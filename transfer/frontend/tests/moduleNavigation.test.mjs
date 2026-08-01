import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const navigationSource = readFileSync(new URL('../src/utils/moduleNavigation.js', import.meta.url), 'utf8')
const taskListSource = readFileSync(new URL('../src/views/TaskList.vue', import.meta.url), 'utf8')
const taskDetailSource = readFileSync(new URL('../src/views/TaskDetail.vue', import.meta.url), 'utf8')

test('Transfer business navigation delegates to the shared Console navigation', () => {
  assert.match(navigationSource, /navigateConsoleModuleRoute\(router, 'transfer', location, options\)/)
  assert.match(taskListSource, /navigateTransferRoute\(router, `\/tasks\/\$\{id\}\/detail`\)/)
  assert.match(taskDetailSource, /navigateTransferRoute\(router, `\/executions\/\$\{executionId\}`\)/)
  assert.doesNotMatch(taskListSource, /router\.(?:push|replace|back)\(/)
  assert.doesNotMatch(taskDetailSource, /router\.(?:push|replace|back)\(/)
})
