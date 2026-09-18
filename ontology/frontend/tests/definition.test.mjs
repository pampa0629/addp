import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import {
  availableActions,
  definitionInput,
  emptyDefinition,
  isActive,
  newMember,
  positiveNumber,
  requiresReconciliation,
  validID
} from '../src/utils/definition.mjs'
import { createOntologyAPI } from '../src/api/ontology.mjs'

test('native definition excludes server scope and owns nested arrays', () => {
  const source = {
    ...emptyDefinition(),
    scope: { tenant_id: 99 },
    classes: [newMember('classes')]
  }
  const input = definitionInput(source)
  assert.equal(input.scope, undefined)
  input.classes[0].parents.push('base')
  assert.deepEqual(source.classes[0].parents, [])
  assert.equal(validID('beijing_outdoor'), true)
  for (const id of ['Beijing', 'a/b', '', '1a'])
    assert.equal(validID(id), false)
  for (const n of ['01', '1e2', '-1', '9007199254740992'])
    assert.equal(positiveNumber(n), false)
})
test('lifecycle permissions are conjunctive; active comes only from head pointer', () => {
  const permissions = ['ontology.revision.update', 'ontology.revision.publish']
  assert.equal(
    availableActions({ status: 'draft' }, null, permissions).save,
    true
  )
  assert.equal(
    availableActions({ status: 'in_review' }, null, permissions).publish,
    false
  )
  permissions.push('system.execution_authorization.create')
  assert.equal(
    availableActions({ status: 'in_review' }, null, permissions).publish,
    true
  )
  assert.equal(
    availableActions({ status: 'published' }, { status: 'failed' }, permissions)
      .rebuild,
    true
  )
  assert.equal(
    availableActions({ status: 'withdrawn' }, { status: 'failed' }, permissions)
      .rebuild,
    false
  )
  const projection = { status: 'ready', generation: 'new', revision: 2 }
  assert.equal(
    isActive({ active_generation: 'old', active_revision: 1 }, projection),
    false
  )
  assert.equal(
    isActive({ active_generation: 'new', active_revision: 2 }, projection),
    true
  )
  assert.equal(
    isActive(
      { active_generation: 'new', active_revision: 2 },
      { ...projection, status: 'pending' }
    ),
    false
  )
})
test('uncertain writes and version conflicts require reconciliation, validation does not', () => {
  for (const status of [409, 500, 502, 504])
    assert.equal(requiresReconciliation({ response: { status } }), true)
  assert.equal(requiresReconciliation(new Error('timeout')), true)
  for (const status of [400, 403, 404])
    assert.equal(requiresReconciliation({ response: { status } }), false)
})
test('API contract preserves pagination envelope, exact version and rebuild baseline', async () => {
  const requests = [],
    envelope = { data: [], total: 0, page: 1, page_size: 20, total_pages: 0 }
  const client = Object.fromEntries(
    ['get', 'put', 'post'].map((method) => [
      method,
      async (...args) => {
        requests.push([method, ...args])
        return { data: envelope }
      }
    ])
  )
  const api = createOntologyAPI(client)
  assert.equal(await api.list(1), envelope)
  await api.create('outdoor', 2, emptyDefinition())
  await api.save('outdoor', 2, 7, emptyDefinition())
  await api.transition('outdoor', 2, 'rebuild', {
    version: 8,
    failed_generation: 'g1',
    activation_version: 5
  })
  assert.deepEqual(requests[0], [
    'get',
    '/ontologies',
    { params: { page: 1, page_size: 20 } }
  ])
  assert.equal(requests[1][2].revision, 2)
  assert.equal(requests[2][2].version, 7)
  assert.deepEqual(requests[3], [
    'post',
    '/ontologies/outdoor/revisions/2/rebuild',
    { version: 8, failed_generation: 'g1', activation_version: 5 }
  ])
})
test('translations have matching leaves and lifecycle/build/CI registration exists before commit', () => {
  const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8')
  const keys = (value, prefix = '') =>
    Object.entries(value)
      .flatMap(([k, v]) =>
        typeof v === 'object' ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`]
      )
      .sort()
  assert.deepEqual(
    keys(JSON.parse(read('../src/i18n/en.json'))),
    keys(JSON.parse(read('../src/i18n/zh-cn.json')))
  )
  const root = '../../..'
  assert.match(
    read(`${root}/scripts/dev/start.sh`),
    /FRONTEND_CONFIGS\+=\("ontology:\$\{ONTOLOGY_FE_PORT\}:ontology\/frontend"\)/
  )
  for (const file of ['build-images.sh', 'push-images.sh', 'package.sh'])
    assert.match(read(`${root}/scripts/build/${file}`), /ontology-frontend/)
  assert.match(
    read(`${root}/.github/workflows/platform-ci.yml`),
    /module: ontology\s+name: Ontology\s+target: test-ontology-frontend\s+playwright: true/
  )
  assert.match(
    read(`${root}/docker-compose.yml`),
    /ontology-frontend:[\s\S]+8123:80/
  )
  const portal = read(`${root}/console/frontend/src/config/portalConfig.js`)
  assert.match(portal, /ontology\.revision\.read/)
  assert.match(portal, /5192\/ontology/)
})
