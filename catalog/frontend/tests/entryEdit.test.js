import { describe, expect, it } from 'vitest'
import {
  buildEntryEditForm,
  buildUpdatePayload,
  governanceOptions,
  hasEffectivePrimaryDomain,
  isCanonicalPositiveID,
  isCanonicalUUID,
  responsibilitySubjectType
} from '../src/utils/entryEdit'

describe('catalog entry edit contract', () => {
  it('maps detail projections into one complete update payload', () => {
    const form = buildEntryEditForm({
      version: 4,
      business_name: ' Orders ',
      business_description: 'Order facts',
      governance_status: 'curated',
      visibility: 'department',
      semantic_links: [
        { semantic_type: 'domain', semantic_id: 10, relation_role: 'primary' },
        { semantic_type: 'glossary', semantic_id: 20, relation_role: 'applies' }
      ],
      responsibilities: [
        { role: 'accountable_department', subject_id: '9007199254740993', status: 'active' }
      ],
      components: [{ id: 'component-1', display_name: 'id', component_status: 'active' }],
      component_elements: [{ component_id: 'component-1', element_id: 50 }]
    })
    const payload = buildUpdatePayload(form)
    expect(payload.version).toBe(4)
    expect(payload.domains).toEqual([{ id: '10', role: 'primary' }])
    expect(payload.glossary_ids).toEqual(['20'])
    expect(payload.responsibilities).toEqual([{
      role: 'accountable_department',
      subject_type: 'department',
      subject_id: '9007199254740993'
    }])
    expect(payload.component_elements).toEqual([{ component_id: 'component-1', element_id: '50' }])
    expect(payload.recommended_successor_entry_id).toBeNull()
  })

  it('only exposes allowed forward governance transitions', () => {
    expect(governanceOptions('discovered', true, true)).toEqual(['discovered', 'curated'])
    expect(governanceOptions('curated', false, true)).toEqual(['curated', 'deprecated'])
    expect(governanceOptions('curated', true, false)).toEqual(['curated', 'certified'])
    expect(governanceOptions('deprecated', true, true)).toEqual(['deprecated'])
  })

  it('keeps System subject IDs as canonical decimal strings', () => {
    expect(isCanonicalPositiveID('9007199254740993')).toBe(true)
    expect(isCanonicalPositiveID('01')).toBe(false)
    expect(responsibilitySubjectType('business_owner')).toBe('user')
  })

  it('keeps the recommended successor in the complete aggregate payload', () => {
    const successorID = '11111111-2222-3333-4444-555555555555'
    const form = buildEntryEditForm({ recommended_successor_entry_id: successorID })
    expect(form.recommendedSuccessorEntryId).toBe(successorID)
    expect(buildUpdatePayload(form).recommended_successor_entry_id).toBe(successorID)
    expect(isCanonicalUUID(successorID)).toBe(true)
    expect(isCanonicalUUID('00000000-0000-0000-0000-000000000000')).toBe(false)
    expect(isCanonicalUUID('11111111-2222-3333-4444-55555555555A')).toBe(false)
  })

  it('keeps Model professional semantics read-only and submits only Catalog-owned links', () => {
    const form = buildEntryEditForm({
      version: 2,
      source: { source_module: 'model', observed_snapshot: { domain_id: '31' } },
      source_resolution: { summary: { domain_id: '32' } },
      semantic_links: [
        { semantic_type: 'domain', semantic_id: 31, relation_role: 'primary' },
        { semantic_type: 'domain', semantic_id: 33, relation_role: 'secondary' }
      ],
      components: [{ id: 'component-1', display_name: 'id', component_status: 'active' }],
      component_elements: [{ component_id: 'component-1', element_id: 50 }]
    })

    expect(form.ownerManagedSemantics).toBe(true)
    expect(form.ownerPrimaryDomainId).toBe('32')
    expect(form.domains).toEqual([{ id: '33', role: 'secondary' }])
    expect(hasEffectivePrimaryDomain(form)).toBe(true)
    expect(buildUpdatePayload(form).component_elements).toEqual([])
  })

  it('keeps Standard Metric professional semantics read-only', () => {
    const form = buildEntryEditForm({
      version: 1,
      source: { source_module: 'standard', source_type: 'metric', observed_snapshot: { domain_id: '41' } },
      semantic_links: [{ semantic_type: 'domain', semantic_id: 42, relation_role: 'secondary' }],
      components: [{ id: 'component-1', display_name: 'value', component_status: 'active' }]
    })

    expect(form.ownerManagedSemantics).toBe(true)
    expect(form.ownerModule).toBe('standard')
    expect(form.ownerPrimaryDomainId).toBe('41')
    expect(form.domains).toEqual([{ id: '42', role: 'secondary' }])
    expect(buildUpdatePayload(form).component_elements).toEqual([])
  })

  it('keeps Service professional components out while Catalog owns the primary domain', () => {
    const form = buildEntryEditForm({
      version: 1,
      source: { source_module: 'service', source_type: 'query_service', observed_snapshot: { name: 'Orders API' } },
      semantic_links: [{ semantic_type: 'domain', semantic_id: 41, relation_role: 'primary' }],
      components: [{ id: 'component-1', display_name: 'value', component_status: 'active' }],
      component_elements: [{ component_id: 'component-1', element_id: 50 }]
    })

    expect(form.ownerManagedSemantics).toBe(false)
    expect(form.ownerManagedComponents).toBe(true)
    expect(form.ownerModule).toBe('service')
    expect(form.domains).toEqual([{ id: '41', role: 'primary' }])
    expect(buildUpdatePayload(form).component_elements).toEqual([])
  })

  it('keeps Develop task internals out while Catalog owns business semantics', () => {
    const form = buildEntryEditForm({
      version: 1,
      source: { source_module: 'develop', source_type: 'dev_task', observed_snapshot: { artifact_type: 'workflow' } },
      semantic_links: [{ semantic_type: 'domain', semantic_id: 51, relation_role: 'primary' }],
      components: [{ id: 'node-1', display_name: 'buffer', component_status: 'active' }]
    })

    expect(form.ownerManagedSemantics).toBe(false)
    expect(form.ownerManagedComponents).toBe(true)
    expect(form.domains).toEqual([{ id: '51', role: 'primary' }])
    expect(buildUpdatePayload(form).component_elements).toEqual([])
  })
})
