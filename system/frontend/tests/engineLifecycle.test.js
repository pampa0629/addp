import { describe, expect, it } from 'vitest'
import { canDisableExtensionEngine, disabledEngineReadbackMatches } from '../src/utils/engineLifecycle'

const engine = { id: '9007199254740993', name: 'Math', engine_origin: 'extension', lifecycle_state: 'active', version: 7 }
describe('extension lifecycle confirmation boundary', () => {
  it('allows built-in extensions while preserving the existing permission and registration boundaries', () => {
    expect(canDisableExtensionEngine({ ...engine, is_builtin: true }, true)).toBe(true)
    expect(canDisableExtensionEngine({ ...engine, is_builtin: false }, true)).toBe(true)
    expect(canDisableExtensionEngine(engine, false)).toBe(false)
    expect(canDisableExtensionEngine({ ...engine, engine_origin: 'general' }, true)).toBe(false)
    for (const lifecycle_state of ['disabled', 'deleting', 'deleted', undefined]) {
      expect(canDisableExtensionEngine({ ...engine, lifecycle_state }, true)).toBe(false)
    }
    for (const version of [undefined, 0, -1, 1.5, '7', Number.MAX_SAFE_INTEGER + 1]) {
      expect(canDisableExtensionEngine({ ...engine, version }, true)).toBe(false)
    }
  })
  it('requires both authoritative projections to identify the same disabled resource at or after the write version', () => {
    const disabled = { ...engine, lifecycle_state: 'disabled', version: 8 }
    expect(disabledEngineReadbackMatches({ ...engine, version: 8 }, disabled, disabled)).toBe(true)
    for (const invalid of [null, { ...disabled, id: '9007199254740992' }, { ...disabled, lifecycle_state: 'active' }, { ...disabled, version: 7 }, { ...disabled, version: '8' }]) {
      expect(disabledEngineReadbackMatches({ ...engine, version: 8 }, invalid, disabled)).toBe(false)
      expect(disabledEngineReadbackMatches({ ...engine, version: 8 }, disabled, invalid)).toBe(false)
    }
  })
})
