import { describe, expect, it } from 'vitest'
import {
  applyEntryNavigationSelection,
  applyUnassignedDepartmentSelection,
  applyUnclassifiedDomainSelection,
  buildEntryFacetQuery
} from '../src/utils/entryNavigation'

describe('Catalog entry navigation', () => {
  it('builds the contextual facet query from canonical route state', () => {
    expect(buildEntryFacetQuery({
      view: 'inventory', primary_domain_id: '10', accountable_department_id: '30',
      entry_type: 'metric', search: 'orders'
    })).toEqual({
      view: 'inventory', primary_domain_id: '10', accountable_department_id: '30', entry_type: 'metric'
    })
  })

  it('clears dependent navigation dimensions when an upstream selection changes', () => {
    const current = {
      view: 'inventory', primary_domain_id: '10', accountable_department_id: '30',
      entry_type: 'metric', source_status: 'active', page: 4, page_size: 50
    }
    expect(applyEntryNavigationSelection(current, 'primary_domain', '20')).toMatchObject({
      primary_domain_id: '20', accountable_department_id: '', entry_type: '',
      source_status: 'active', page: 1, page_size: 50
    })
    expect(applyEntryNavigationSelection(current, 'accountable_department', '40')).toMatchObject({
      primary_domain_id: '10', accountable_department_id: '40', entry_type: '', page: 1
    })
    expect(applyEntryNavigationSelection(current, 'entry_type', 'data_service')).toMatchObject({
      primary_domain_id: '10', accountable_department_id: '30', entry_type: 'data_service', page: 1
    })
  })

  it('keeps ordinary navigation and governance gap states mutually exclusive', () => {
    expect(applyEntryNavigationSelection({
      view: 'inventory', coverage_dimension: 'primary_domain', coverage_state: 'missing'
    }, 'primary_domain', '20')).toMatchObject({
      primary_domain_id: '20', coverage_dimension: '', coverage_state: '', page: 1
    })
  })

  it('maps the unclassified domain action to the canonical primary-domain gap', () => {
    expect(applyUnclassifiedDomainSelection({
      view: 'governance', search: 'orders', primary_domain_id: '10',
      accountable_department_id: '30', entry_type: 'metric', source_status: 'active', page: 4
    })).toMatchObject({
      view: 'inventory', search: '', primary_domain_id: '', accountable_department_id: '',
      entry_type: '', source_status: 'active', coverage_dimension: 'primary_domain',
      coverage_state: 'missing', page: 1
    })
  })

  it('preserves the selected domain when opening the unassigned-department gap', () => {
    expect(applyUnassignedDepartmentSelection({
      view: 'inventory', search: 'orders', primary_domain_id: '10',
      accountable_department_id: '30', entry_type: 'metric', source_status: 'active', page: 4
    })).toMatchObject({
      view: 'inventory', search: '', primary_domain_id: '10', accountable_department_id: '',
      entry_type: '', source_status: 'active', coverage_dimension: 'accountable_department',
      coverage_state: 'missing', page: 1
    })
  })
})
