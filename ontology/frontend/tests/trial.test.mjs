import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import {
  sameBinding,
  trialInputs,
  trialOptions,
  trialOwner,
  trialRequest
} from '../src/utils/trial.mjs'
import { createOntologyAPI } from '../src/api/ontology.mjs'

const binding = {
  ontology_id: 'outdoor',
  revision: 1,
  generation: 'g',
  activation_version: 2,
  digest: 'd',
  knowledge_kind: 'native_definition'
}
test('hypothesis ownership uses verified principal, tenant and membership, never a token', () => {
  const auth = {
    principal: { type: 'user', id: '1' },
    context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '3' }
  }
  const owner = trialOwner(auth)
  assert.notEqual(owner, '')
  assert.equal(trialOwner({ ...auth, token: { id: 'rotated' } }), owner)
  assert.equal(trialOwner(null), '')
  for (const [parent, field] of [
    ['principal', 'id'],
    ['context', 'tenant_id'],
    ['context', 'tenant_membership_id']
  ]) {
    const changed = structuredClone(auth)
    changed[parent][field] = '4'
    assert.notEqual(trialOwner(changed), owner)
    delete changed[parent][field]
    assert.equal(trialOwner(changed), '')
  }
  assert.equal(
    trialOwner({ ...auth, principal: { type: 'service_principal', id: '1' } }),
    ''
  )
  assert.equal(trialOwner({ ...auth, context: { type: 'platform' } }), '')
})
test('trial adapter never supplies values for unobserved states or runs CEL', () => {
  const property = { id: 'date', kind: 'bool', enum: [] }
  const rows = trialInputs(
    {
      inputs: [
        { variable: 'has_date', property_id: 'date', on_absent: 'false' }
      ]
    },
    [property]
  )
  assert.equal(rows[0].state, 'unknown')
  assert.deepEqual(trialRequest(binding, rows).inputs, {
    has_date: { state: 'unknown' }
  })
  rows[0].state = 'known'
  assert.deepEqual(trialRequest(binding, rows).inputs, {
    has_date: { state: 'known', value: false }
  })
  for (const state of ['absent', 'unknown', 'invalid']) {
    rows[0].state = state
    assert.deepEqual(trialRequest(binding, rows).inputs, {
      has_date: { state }
    })
  }
  assert.equal(trialRequest(binding, rows).tenant_id, undefined)
  assert.deepEqual(trialOptions({ enum: ['报名中'] }), [
    { value: '报名中', labels: { 'zh-cn': '报名中', en: '报名中' } }
  ])
  assert.throws(() => trialInputs({ inputs: [{ property_id: 'missing' }] }, []))
})
test('activation binding compares every identity component', () => {
  assert.equal(sameBinding(binding, { ...binding }), true)
  for (const key of Object.keys(binding))
    assert.equal(sameBinding(binding, { ...binding, [key]: 'changed' }), false)
  assert.equal(sameBinding({}, {}), false)
})
test('only the canonical API path is used; binding is explicit and cancellation propagated', async () => {
  const requests = []
  const client = Object.fromEntries(
    ['get', 'post'].map((method) => [
      method,
      async (...args) => {
        requests.push([method, ...args])
        return { data: binding }
      }
    ])
  )
  const api = createOntologyAPI(client),
    signal = new AbortController().signal
  await api.classes('outdoor')
  await api.classContext('outdoor', 'activity', binding)
  const body = trialRequest(binding, [])
  await api.trial('outdoor', 'valid_date', body, signal)
  assert.deepEqual(requests, [
    ['get', '/ontologies/outdoor/semantic/classes'],
    [
      'get',
      '/ontologies/outdoor/semantic/classes/activity',
      { params: { revision: 1, generation: 'g', activation_version: 2 } }
    ],
    [
      'post',
      '/ontologies/outdoor/semantic/rules/valid_date/trial',
      body,
      { signal }
    ]
  ])
  const page = readFileSync(
    new URL('../src/views/RuleTrial.vue', import.meta.url),
    'utf8'
  )
  assert.match(page, /ParameterValueInput/)
  assert.match(page, /StatusAnnouncer/)
  assert.doesNotMatch(page, /eval\(|new Function|localStorage|sessionStorage/)
})
