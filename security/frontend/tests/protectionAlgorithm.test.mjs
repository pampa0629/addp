import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { prefixAlgorithm, constantAlgorithm, sm3Algorithm, initialProtectionConfiguration, validateProtectionConfiguration } from '../src/utils/protectionAlgorithm.mjs'

const catalog = [
  { key: prefixAlgorithm, supported_field_types: ['string'] },
  { key: constantAlgorithm, supported_field_types: ['string', 'bool', 'int', 'bigint', 'float', 'double'] },
  { key: sm3Algorithm, supported_field_types: ['string'] }
]

describe('field algorithms and one editor', () => {
  it('requires explicit default permission and does not infer cross-algorithm strength', () => {
    const configuration = initialProtectionConfiguration()
    expect(validateProtectionConfiguration(configuration, catalog, { defaults: true })).toBe(true)
    configuration.allowed_algorithms = [constantAlgorithm, sm3Algorithm]
    expect(validateProtectionConfiguration(configuration, catalog, { defaults: true })).toBe(false)
    configuration.algorithm = sm3Algorithm
    configuration.parameters = {}
    expect(validateProtectionConfiguration(configuration, catalog, { defaults: true })).toBe(true)
    configuration.allowed_algorithms.push(prefixAlgorithm)
    expect(validateProtectionConfiguration(configuration, catalog, { defaults: true })).toBe(false)
  })

  it('limits independent prefix retention and rejects geometry hashing', () => {
    const baseline = initialProtectionConfiguration()
    baseline.allowed_algorithms.push(sm3Algorithm)
    const field = initialProtectionConfiguration()
    field.parameters.prefix_runes = 4
    expect(validateProtectionConfiguration(field, catalog, { baseline, fieldType: 'string' })).toBe(false)
    field.parameters.prefix_runes = 2
    expect(validateProtectionConfiguration(field, catalog, { baseline, fieldType: 'string' })).toBe(true)
    field.algorithm = sm3Algorithm
    field.parameters = {}
    expect(validateProtectionConfiguration(field, catalog, { baseline, fieldType: 'string' })).toBe(true)
    expect(validateProtectionConfiguration(field, catalog, { baseline, fieldType: 'geometry' })).toBe(false)
    field.parameters = { salt: 'unsupported' }
    expect(validateProtectionConfiguration(field, catalog, { baseline, fieldType: 'string' })).toBe(false)
  })

  it('preserves replacement types and has exactly one editor for every configuration entry', () => {
    const field = { algorithm: constantAlgorithm, parameters: { value: 0 } }
    expect(validateProtectionConfiguration(field, catalog, { fieldType: 'int' })).toBe(true)
    expect(validateProtectionConfiguration(field, catalog, { fieldType: 'string' })).toBe(false)
    field.parameters.value = false
    expect(validateProtectionConfiguration(field, catalog, { fieldType: 'bool' })).toBe(true)
    for (const path of ['views/ProtectionBaselineBindings.vue', 'views/SensitiveDataTypeList.vue', 'components/protection-enrollment/ProtectionPolicyForm.vue']) {
      const source = readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8')
      expect(source).toContain('<ProtectionAlgorithmEditor')
      expect(source).not.toContain('v-model="form.parameters.prefix_runes"')
    }
  })
})
