import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const navigationSource = readFileSync(new URL('../src/utils/moduleNavigation.js', import.meta.url), 'utf8')
const listSource = readFileSync(new URL('../src/views/OrchestrationList.vue', import.meta.url), 'utf8')
const formSource = readFileSync(new URL('../src/views/OrchestrationForm.vue', import.meta.url), 'utf8')

test('Orchestrator business navigation delegates to the shared Console navigation', () => {
  assert.match(navigationSource, /navigateConsoleModuleRoute\(router, 'orchestrator', location, options\)/)
  assert.match(listSource, /navigateOrchestratorRoute\(router, `\/orchestrations\/\$\{row\.id\}\/edit`\)/)
  assert.match(formSource, /navigateOrchestratorRoute\(router, '\/orchestrations', \{ history: 'replace' \}\)/)
  assert.doesNotMatch(listSource, /router\.(?:push|replace|back)\(/)
  assert.doesNotMatch(formSource, /router\.(?:push|replace|back)\(/)
})
