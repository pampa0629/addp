import { describe, expect, it } from 'vitest'

import {
  STANDARD_PERMISSION_RESOURCES,
  buildStandardPermission
} from '../src/utils/standardPermissions'

describe('Standard permissions', () => {
  it('builds canonical permission keys for every Standard resource', () => {
    expect(STANDARD_PERMISSION_RESOURCES.map(resource => buildStandardPermission(resource, 'update'))).toEqual([
      'standard.code_set.update',
      'standard.dimension_hierarchy.update',
      'standard.document.update',
      'standard.domain.update',
      'standard.element.update',
      'standard.glossary.update',
      'standard.metric.update',
      'standard.unit.update'
    ])
  })

  it('rejects aliases and unknown resources instead of creating compatibility keys', () => {
    expect(() => buildStandardPermission('code-set', 'read')).toThrow('Unknown Standard permission resource')
    expect(() => buildStandardPermission('dimension', 'read')).toThrow('Unknown Standard permission resource')
  })
})
