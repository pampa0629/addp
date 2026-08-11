import { describe, expect, it, vi } from 'vitest'

import { translateDynamicKey } from '../src/utils/configurationI18n'

describe('translateDynamicKey', () => {
  it('does not ask vue-i18n to translate Element Plus measurement rows', () => {
    const translate = vi.fn(key => key)

    expect(translateDynamicKey(translate, 'console.configuration.ai.scenarios', undefined)).toBe('')
    expect(translate).not.toHaveBeenCalled()
  })

  it('translates a complete dynamic configuration key', () => {
    const translate = vi.fn(key => `translated:${key}`)

    expect(translateDynamicKey(
      translate,
      'console.configuration.ai.scenarios',
      'resource_resolution'
    )).toBe('translated:console.configuration.ai.scenarios.resource_resolution')
    expect(translate).toHaveBeenCalledOnce()
  })
})
