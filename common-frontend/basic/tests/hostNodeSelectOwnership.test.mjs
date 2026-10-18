import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync, existsSync } from 'node:fs'

test('managed host selection and manifest parsing have one shared owner with API and authority supplied by consumers', () => {
  const shared = readFileSync(new URL('../src/components/HostNodeSelect.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(shared, /useAuthStore|from .*monitor\/|from .*system\//)
  for (const path of ['monitor/frontend/src/views/MonitoringTargets.vue', 'monitor/frontend/src/components/ResourceAlertRules.vue', 'system/frontend/src/views/HostAdmission.vue']) {
    const source = readFileSync(new URL(`../../../${path}`, import.meta.url), 'utf8')
    assert.match(source, /common-frontend\/basic\/src\/components\/HostNodeSelect.vue/)
    assert.match(source, /:api=/)
    assert.match(source, /:allowed=/)
  }
  assert.equal(existsSync(new URL('../../../monitor/frontend/src/components/HostNodeSelect.vue', import.meta.url)), false)
  assert.equal(existsSync(new URL('../../../monitor/frontend/src/utils/hostAdmission.js', import.meta.url)), false)
  const monitor = readFileSync(new URL('../../../monitor/frontend/src/views/MonitoringTargets.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(monitor, /createNode|admission-file|importAdmission/)
})
