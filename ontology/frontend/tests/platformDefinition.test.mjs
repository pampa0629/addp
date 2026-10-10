import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { platformGraph, platformInspectionOwner, validCapability } from '../src/utils/platformDefinition.mjs'
import { createOntologyAPI } from '../src/api/ontology.mjs'

test('platform graph adapts every declared concept and relation without instance inference', () => {
  const definition = JSON.parse(readFileSync(new URL('../../backend/internal/platform/transfer.json', import.meta.url)))
  const graph = platformGraph(definition, 'en', { node: 'primary', edge: 'secondary' })
  assert.equal(graph.entityTypes.length, 15)
  assert.equal(graph.relationTypes.length, 14)
  assert.equal(graph.entityTypes[0].id, definition.concepts[0].id)
  assert.equal(graph.entityTypes[0].label, definition.concepts[0].name.en)
  assert.equal(new Set(graph.relationTypes.map(item => item.id)).size, 14)
  assert.equal(graph.relationTypes[0].source_type_id, definition.relations[0].from)
  const page = readFileSync(new URL('../src/views/PlatformDefinitions.vue', import.meta.url), 'utf8')
  assert.match(page, /common-frontend\/graph\/src\/OntologyView\.vue/)
  assert.match(page, /readonly @node-click/)
  assert.doesNotMatch(page, /new G6|GRAPH\.QUERY|localStorage|postMessage/)
})

test('inspection owner excludes Tenant and machine sessions; identities follow the backend grammar', () => {
  assert.equal(platformInspectionOwner({ principal:{type:'user',id:'9'},context:{type:'platform'} }), '9')
  for (const context of [null,{principal:{type:'user',id:'9'},context:{type:'tenant'}},{principal:{type:'service_principal',id:'9'},context:{type:'platform'}}]) assert.equal(platformInspectionOwner(context),'')
  for (const value of ['transfer.task.create','other.operation']) assert.equal(validCapability(value),true)
  for (const value of ['',null,'Transfer.task.create','task','task/a','task.create?x=1','a'.repeat(129)+'.task']) assert.equal(validCapability(value),false)
})

test('inspection API uses the sole readonly endpoint and cancellation, never a source-file request', async () => {
  const signal = new AbortController().signal, requests = []
  const client = { get:async(path,config) => { requests.push({path,config}); return {data:{test:true}} } }
  const api = createOntologyAPI(client)
  assert.deepEqual(await api.platformDefinitions(signal), {test:true})
  await api.platformDefinition('other.operation',signal)
  assert.deepEqual(requests.map(item => item.path), ['/platform/definitions','/platform/definitions/other.operation'])
  assert.equal(requests[1].config.signal, signal)
})
