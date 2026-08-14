import test from 'node:test'
import assert from 'node:assert/strict'
import { filterERDiagramByDomain } from '../src/utils/erDiagramState.js'

const entities = [
  { id: 1, domain_id: 2, code: 'CUSTOMER' },
  { id: 2, domain_id: 2, code: 'ORDER' },
  { id: 3, domain_id: 1, code: 'PRODUCT' }
]

const relations = [
  { id: 11, source_entity: 1, target_entity: 2 },
  { id: 12, source_entity: 2, target_entity: 3 },
  { id: 13, source_entity: 3, target_entity: 3 },
  { id: 14, source_entity: 99, target_entity: 1 }
]

test('business-domain filter keeps only entities and fully visible relations in that domain', () => {
  assert.deepEqual(filterERDiagramByDomain(entities, relations, 2), {
    entities: entities.slice(0, 2),
    relations: [relations[0]]
  })
})

test('unfiltered ER diagram still rejects relations whose endpoints are missing', () => {
  assert.deepEqual(filterERDiagramByDomain(entities, relations), {
    entities,
    relations: relations.slice(0, 3)
  })
})

test('unknown business domain returns a coherent empty diagram', () => {
  assert.deepEqual(filterERDiagramByDomain(entities, relations, 999), {
    entities: [],
    relations: []
  })
})
