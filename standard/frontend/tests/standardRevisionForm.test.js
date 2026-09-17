import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import {
  buildCodeSetRevisionPayload,
  buildElementRevisionPayload,
  isCodeSetCompatible,
  listReplacementItems,
  resetIncompatibleElementConstraints
} from '../src/utils/standardRevisionForm'

describe('Standard revision form mapping', () => {
  it('matches backend code set value-type compatibility', () => {
    expect(isCodeSetCompatible('string', 'string')).toBe(true)
    expect(isCodeSetCompatible('text', 'string')).toBe(false)
    expect(isCodeSetCompatible('int', 'bigint')).toBe(true)
    expect(isCodeSetCompatible('bigint', 'int')).toBe(false)
    expect(isCodeSetCompatible('decimal', 'string')).toBe(false)
  })

  it('clears constraints that become invalid after changing data type', () => {
    const revision = {
      length: 32,
      precision_num: 10,
      scale: 2,
      format: '^x$',
      value_domain_kind: 'range',
      range_constraint: { min: 0, max: 10 },
      code_set_revision_id: 7
    }

    resetIncompatibleElementConstraints(revision, 'bool')

    expect(revision).toMatchObject({
      length: null,
      precision_num: null,
      scale: null,
      format: '',
      value_domain_kind: 'unrestricted',
      range_constraint: null,
      code_set_revision_id: null
    })
  })

  it('submits semantic constraints without an independently editable rule document', () => {
    const payload = buildElementRevisionPayload({
      name: 'Amount',
      definition: 'Transaction amount',
      data_type: 'decimal',
      precision_num: 12,
      scale: 2,
      nullable: false,
      value_domain_kind: 'range',
      range_constraint: { min: 0, min_inclusive: true },
      example_values: ['12.50'],
      change_summary: 'Define amount',
      effective_from: '2026-09-01T00:00:00+08:00'
    }, 4)

    expect(payload.version).toBe(4)
    expect(payload.code_set_revision_id).toBeNull()
    expect(payload).not.toHaveProperty('extra_quality_rules')
    expect(payload).not.toHaveProperty('compiled_quality_rules')
    expect(payload.effective_to).toBeNull()
  })

  it('maps code set revision effective dates without identity fields', () => {
    expect(buildCodeSetRevisionPayload({
      name: 'Gender',
      description: 'Gender code list',
      value_type: 'string',
      change_summary: 'Initial definition',
      effective_from: null,
      effective_to: '2027-01-01T00:00:00+08:00'
    }, 2)).toEqual({
      version: 2,
      name: 'Gender',
      description: 'Gender code list',
      value_type: 'string',
      change_summary: 'Initial definition',
      effective_from: null,
      effective_to: '2027-01-01T00:00:00+08:00'
    })
  })

  it('only offers other active items as replacements', () => {
    expect(listReplacementItems([
      { id: 1, status: 'deprecated' },
      { id: 2, status: 'active' },
      { id: 3, status: 'active' }
    ], 2)).toEqual([{ id: 3, status: 'active' }])
  })
})

describe('measurement unit selector ownership', () => {
  it('uses one grouped selector in every Standard unit field', () => {
    for (const name of ['ElementDetail', 'MetricList', 'MetricDetail']) {
      const source = readFileSync(new URL(`../src/views/${name}.vue`, import.meta.url), 'utf8')
      expect(source).toContain('<UnitSelect')
      expect(source).not.toContain('v-for="unit in units"')
    }
  })
})


describe('Standard definition presentation ownership', () => {
  it('uses one type selector and removes the unused steward field from every standard form', () => {
    for (const view of ['ElementList', 'ElementDetail']) {
      const source = readFileSync(new URL(`../src/views/${view}.vue`, import.meta.url), 'utf8')
      expect(source).toContain('<ElementDataTypeSelect')
      expect(source).not.toContain('v-for="type in')
    }
    for (const view of ['ElementList', 'ElementDetail', 'GlossaryDetail', 'CodeSetDetail', 'MetricList', 'MetricDetail', 'DocumentDetail']) {
      const source = readFileSync(new URL(`../src/views/${view}.vue`, import.meta.url), 'utf8')
      expect(source).not.toContain('steward_id')
      expect(source).not.toContain('stewardId')
    }
  })
})
