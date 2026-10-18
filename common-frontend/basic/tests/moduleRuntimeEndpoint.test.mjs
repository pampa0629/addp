import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { getRegisteredEndpoint } from '../src/utils/moduleRuntimeEndpoint.mjs'

test('registered endpoints preserve DNS, IPv4, IPv6 and protocol ports without URL secrets', () => {
  for (const [address, expected] of [
    ['http://manager:8081/api', 'manager:8081'],
    ['http://192.0.2.7:8180', '192.0.2.7:8180'],
    ['http://[2001:db8::7]:8180', '[2001:db8::7]:8180'],
    ['https://example.test', 'example.test:443'],
    ['http://example.test', 'example.test:80'],
    ['https://user:secret@example.test:8443/private?token=secret#fragment', 'example.test:8443']
  ]) assert.equal(getRegisteredEndpoint({ module_url: address }), expected)
  assert.equal(getRegisteredEndpoint({ health_check_url: 'http://health:8090/ready' }), 'health:8090')
  assert.equal(getRegisteredEndpoint({ module_url: 'https://service.test', health_check_url: 'http://health:8090' }), 'service.test:443')
})

test('missing or malformed listeners cannot be fabricated from runtime or deployment addresses', () => {
  for (const instance of [
    null, { role: 'worker', registered_host: '192.0.2.7', runtime_hostname: 'container', host_node_ips: ['192.0.2.8'] },
    { module_url: 'broken', health_check_url: 'http://health:8090' },
    { module_url: 'ftp://example.test:21' }, { module_url: ['http://example.test'] }
  ]) assert.equal(getRegisteredEndpoint(instance), null)
})

test('System and Monitor consume the sole registered endpoint parser', () => {
  for (const path of ['../../../system/frontend/src/components/ModuleInstances.vue', '../../../system/frontend/src/views/Modules.vue', '../../../monitor/frontend/src/views/ProcessResources.vue']) {
    const source = readFileSync(new URL(path, import.meta.url), 'utf8')
    assert.match(source, /import \{ getRegisteredEndpoint \} from ['"].*moduleRuntimeEndpoint\.mjs['"]/)
    assert.match(source, /getRegisteredEndpoint\((row|instance)\)/)
  }
  const oldOwner = readFileSync(new URL('../../../system/frontend/src/utils/moduleRuntimePresentation.js', import.meta.url), 'utf8')
  assert.doesNotMatch(oldOwner, /getRegisteredEndpoint/)
})
