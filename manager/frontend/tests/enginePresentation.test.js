import { describe, expect, it } from 'vitest'
import { normalizeEngineCatalog, resolveEngineName } from '../src/utils/enginePresentation.js'

describe('engine presentation', () => {
  it('normalizes supported engine catalog response wrappers', () => {
    const engines = [{ id: 12, name: 'Business MinIO' }]

    expect(normalizeEngineCatalog({ data: engines })).toEqual(engines)
    expect(normalizeEngineCatalog({ data: { data: engines } })).toEqual(engines)
  })

  it('resolves an engine name without exposing an unknown numeric id', () => {
    const engines = [{ id: 12, name: 'Business MinIO' }]

    expect(resolveEngineName(engines, 12)).toBe('Business MinIO')
    expect(resolveEngineName(engines, 99)).toBe('')
  })
})
