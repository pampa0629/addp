import { describe, expect, it } from 'vitest'
import {
  BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT,
  BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN,
  BATCH_GOVERNANCE_MAX_ENTRIES,
  buildBatchGovernancePayload,
  unsupportedPrimaryDomainEntries
} from '../src/utils/batchGovernance'

const first = { id: '11111111-1111-4111-8111-111111111111', version: 2, entry_type: 'data_item' }
const second = { id: '22222222-2222-4222-8222-222222222222', version: 5, entry_type: 'data_service' }

describe('Catalog batch governance', () => {
  it('builds an explicit member payload without filter state', () => {
    expect(buildBatchGovernancePayload([second, first], BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT, '42')).toEqual({
      entries: [{ id: second.id, version: 5 }, { id: first.id, version: 2 }],
      operation: BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT,
      reference_id: '42'
    })
  })

  it('identifies owner-managed primary-domain entry types', () => {
    const rows = [first, { ...second, entry_type: 'business_entity' }, { ...second, id: '33333333-3333-4333-8333-333333333333', entry_type: 'metric' }]
    expect(unsupportedPrimaryDomainEntries(rows).map(row => row.entry_type)).toEqual(['business_entity', 'metric'])
    expect(() => buildBatchGovernancePayload(rows, BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN, '7')).toThrow('owner-managed primary domain')
  })

  it('rejects implicit, duplicate, malformed, or oversized member sets', () => {
    expect(() => buildBatchGovernancePayload([], BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT, '1')).toThrow()
    expect(() => buildBatchGovernancePayload([first, first], BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT, '1')).toThrow()
    expect(() => buildBatchGovernancePayload([first], BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT, '01')).toThrow()
    expect(() => buildBatchGovernancePayload(Array.from({ length: BATCH_GOVERNANCE_MAX_ENTRIES + 1 }, () => first), BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT, '1')).toThrow()
  })
})
