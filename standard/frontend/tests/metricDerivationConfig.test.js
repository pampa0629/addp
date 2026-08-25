import { describe, expect, it } from 'vitest'
import { parseMetricDerivationConfig, stringifyMetricDerivationConfig } from '../src/utils/metricDerivationConfig'

describe('metric derivation config serialization', () => {
  it('formats an object as editable JSON and parses it back', () => {
    const config = { version: 1, plan: { collection: 'Outdoors' } }
    const text = stringifyMetricDerivationConfig(config)

    expect(text).toContain('"collection": "Outdoors"')
    expect(parseMetricDerivationConfig(text)).toEqual(config)
  })

  it.each([null, undefined, ''])('returns null for empty config %s', value => {
    expect(parseMetricDerivationConfig(value)).toBeNull()
  })

  it('rejects non-object JSON values', () => {
    expect(() => parseMetricDerivationConfig('[]')).toThrow('JSON object')
    expect(() => parseMetricDerivationConfig('invalid')).toThrow()
  })
})
