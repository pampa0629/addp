import { describe, expect, it } from 'vitest'
import {
  normalizeProfessionalRelations,
  professionalRelationFailureState,
  professionalNodesToSourceReferences,
  professionalResourceKey,
  resolveProfessionalRelationSubject,
  sourceEntryResolutionMap
} from '../src/utils/professionalRelationView'

describe('professional relation federated view', () => {
  it('resolves only supported active owner sources to user-context routes', () => {
    expect(resolveProfessionalRelationSubject({
      entry_status: 'active',
      source: { source_status: 'active', source_module: 'model', source_type: 'entity', source_identity: '12' }
    })).toEqual({
      key: 'model:entity:12',
      owner_module: 'model',
      resource_type: 'entity',
      resource_id: '12',
      path: '/model/entities/12/relations',
      permissions: ['model.entity.read', 'model.entity_relation.read']
    })
    expect(resolveProfessionalRelationSubject({
      entry_status: 'active',
      source: { source_status: 'active', source_module: 'standard', source_type: 'metric', source_identity: '8' }
    })?.path).toBe('/standard/metrics/8/relations')
  })

  it('does not guess unsupported, missing, merged, or malformed subjects', () => {
    for (const entry of [
      { entry_status: 'merged', source: { source_status: 'active', source_module: 'model', source_type: 'entity', source_identity: '1' } },
      { entry_status: 'active', source: { source_status: 'missing', source_module: 'model', source_type: 'entity', source_identity: '1' } },
      { entry_status: 'active', source: { source_status: 'active', source_module: 'service', source_type: 'query_service', source_identity: '1' } },
      { entry_status: 'active', source: { source_status: 'active', source_module: 'model', source_type: 'entity', source_identity: '01' } }
    ]) {
      expect(resolveProfessionalRelationSubject(entry)).toBeNull()
    }
  })

  it('normalizes owner responses and preserves namespaced identities', () => {
    const graph = normalizeProfessionalRelations({
      schema_version: 'addp.professional_relations/v1',
      subject: { owner_module: 'model', resource_type: 'entity', resource_id: '1' },
      nodes: [{ owner_module: 'model', resource_type: 'entity', resource_id: '1' }],
      edges: [{ relation_kind: 'model.entity.one_to_many' }],
      truncated: true
    })
    expect(graph.edges).toHaveLength(1)
    expect(graph.truncated).toBe(true)
    expect(professionalResourceKey(graph.subject)).toBe('model:entity:1')
  })

  it('keeps permission, missing subject, and owner outage distinct', () => {
    expect(professionalRelationFailureState({ response: { status: 403 } })).toBe('forbidden')
    expect(professionalRelationFailureState({ response: { status: 404 } })).toBe('subject_missing')
    expect(professionalRelationFailureState(new Error('offline'))).toBe('unavailable')
  })

  it('maps only supported stable owner nodes to Catalog source references', () => {
    expect(professionalNodesToSourceReferences([
      { owner_module: 'model', resource_type: 'entity', resource_id: '12' },
      { owner_module: 'model', resource_type: 'entity', resource_id: '12' },
      { owner_module: 'standard', resource_type: 'metric', resource_id: '8' },
      { owner_module: 'standard', resource_type: 'domain', resource_id: '3' },
      { owner_module: 'model', resource_type: 'entity', resource_id: '01' }
    ])).toEqual([
      { source_module: 'model', source_type: 'entity', source_identity: '12' },
      { source_module: 'standard', source_type: 'metric', source_identity: '8' }
    ])
    const mapping = sourceEntryResolutionMap([
      { source_module: 'model', source_type: 'entity', source_identity: '12', found: true, entry: { id: 'catalog-12' } },
      { source_module: 'standard', source_type: 'metric', source_identity: '8', found: false }
    ])
    expect(mapping.get('model:entity:12')).toEqual({ id: 'catalog-12' })
    expect(mapping.has('standard:metric:8')).toBe(false)
  })
})
