import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync, existsSync, readdirSync } from 'node:fs'
import { resolve, relative } from 'node:path'

const root = resolve(import.meta.dirname, '../../..')
const source = path => readFileSync(resolve(root, path), 'utf8')
const consumers = [
  'monitor/frontend/src/components/ResourceAlertRuleEditor.vue',
  'monitor/frontend/src/views/MonitoringTargets.vue',
  'system/frontend/src/views/EngineDeployments.vue',
  'system/frontend/src/views/HostAdmission.vue'
]

test('managed host selection and manifest parsing have one shared owner with API and authority supplied by consumers', () => {
  const shared = source('common-frontend/basic/src/components/HostNodeSelect.vue')
  assert.doesNotMatch(shared, /useAuthStore|from .*monitor\/|from .*system\//)
  assert.match(source('common-frontend/basic/src/index.js'), /export \{ default as HostNodeSelect \} from '\.\/components\/HostNodeSelect\.vue'/)
  // Cover every actual consumer, including the canonical public re-export.
  const roots = readdirSync(root, { withFileTypes: true }).filter(entry => entry.isDirectory())
    .map(entry => resolve(root, entry.name, 'frontend/src')).filter(existsSync)
  roots.push(resolve(root, 'common-frontend/basic/src'))
  const actual = roots.flatMap(dir => readdirSync(dir, { recursive: true, withFileTypes: true })
    .filter(entry => entry.isFile() && entry.name.endsWith('.vue'))
    .map(entry => resolve(entry.parentPath, entry.name)))
    .filter(path => /<HostNodeSelect\b/u.test(readFileSync(path, 'utf8')))
    .map(path => relative(root, path)).sort()
  assert.deepEqual(actual, consumers)
  for (const path of consumers) {
    const consumer = source(path)
    assert.match(consumer, /import HostNodeSelect from ['"](?:\.\.\/)+common-frontend\/basic\/src\/components\/HostNodeSelect\.vue['"]|import\s*\{[^}]*\bHostNodeSelect\b[^}]*\}\s*from\s*['"]@common-ui['"]/u, path)
    const uses = [...consumer.matchAll(/<HostNodeSelect\b[\s\S]*?\/>/gu)]
    assert.ok(uses.length > 0, path)
    for (const [tag] of uses) {
      assert.match(tag, /\s:api="[^"]+"/u, path)
      assert.match(tag, /\s:allowed="[^"]+"/u, path)
      assert.match(tag, /\s:identity="[^"]+"/u, path)
    }
  }
  assert.equal(existsSync(new URL('../../../monitor/frontend/src/components/HostNodeSelect.vue', import.meta.url)), false)
  assert.equal(existsSync(new URL('../../../monitor/frontend/src/utils/hostAdmission.js', import.meta.url)), false)
  const monitor = readFileSync(new URL('../../../monitor/frontend/src/views/MonitoringTargets.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(monitor, /createNode|admission-file|importAdmission/)
})

test('Monitor rule list and host mounts compose the sole rule editor instead of owning another picker', () => {
  for (const path of ['monitor/frontend/src/components/ResourceAlertRules.vue', 'monitor/frontend/src/views/NodeResources.vue']) {
    const consumer = source(path)
    assert.match(consumer, /import ResourceAlertRuleEditor from ['"](?:\.\/|\.\.\/components\/)ResourceAlertRuleEditor\.vue['"]/u, path)
    assert.match(consumer, /<ResourceAlertRuleEditor\b/u, path)
    assert.doesNotMatch(consumer, /<HostNodeSelect\b|data-testid="rule-name"|api\.(?:createRule|updateRule)\(/u, path)
  }
})
