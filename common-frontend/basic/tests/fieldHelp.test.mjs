import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { debounce } from '../src/utils/index.js'

test('debounced work keeps the latest value and can be cancelled before leaving its owner', t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const calls = [], apply = debounce(value => calls.push(value))
  apply('m'); t.mock.timers.tick(100); apply('meta')
  t.mock.timers.tick(299); assert.deepEqual(calls, [])
  t.mock.timers.tick(1); assert.deepEqual(calls, ['meta'])
  apply('old-scope'); apply.cancel(); t.mock.timers.tick(1000)
  assert.deepEqual(calls, ['meta'])
  apply('new-scope'); t.mock.timers.tick(300)
  assert.deepEqual(calls, ['meta', 'new-scope'])
})

test('service and shared time explanations have one accessible FieldHelp owner', () => {
  const help = readFileSync(new URL('../src/components/FieldHelp.vue', import.meta.url), 'utf8')
  assert.match(help, /:aria-label="content"/)
  assert.match(help, /type="button"/)
  assert.match(help, /\['hover', 'focus'\]/)
  for (const path of ['../src/components/RuntimeInstanceTimeFilter.vue', '../../../monitor/frontend/src/views/ProcessResources.vue']) {
    const source = readFileSync(new URL(path, import.meta.url), 'utf8')
    assert.match(source, /import FieldHelp from/)
    assert.match(source, /<FieldHelp/)
    assert.doesNotMatch(source, /<p[^>]*>(?:\{\{ t\('monitor.process\.(?:address|hostName|hostIP|declaration)Hint|\{\{ t\(`common.runtimeInstanceTime\.\$\{timeBasis\}Hint)/)
  }
})
