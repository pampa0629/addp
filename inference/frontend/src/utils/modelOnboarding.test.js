import { describe, expect, it } from 'vitest'
import {
  applyPreset,
  createModelDraft,
  isValidProfileCode,
  modelOptions,
  parseTenantIDs
} from './modelOnboarding'

describe('model onboarding helpers', () => {
  it('applies stable capability presets', () => {
    const draft = applyPreset(createModelDraft(), 'multimodal_embedding_2560')
    expect(draft).toMatchObject({
      preset: 'multimodal_embedding_2560',
      dimension: 2560,
      profileCode: 'multimodal-embedding'
    })
  })

  it('normalizes tenant allowlists', () => {
    expect(parseTenantIDs('1, 2, 1, invalid, 0')).toEqual([1, 2])
  })

  it('merges discovered and suggested model identifiers', () => {
    expect(modelOptions([{ id: 'model-b' }, { id: 'model-a' }], [{ upstream_model: 'model-b' }, { upstream_model: 'model-c' }]))
      .toEqual(['model-a', 'model-b', 'model-c'])
  })

  it('validates stable profile codes', () => {
    expect(isValidProfileCode('chat-default')).toBe(true)
    expect(isValidProfileCode('Chat_Default')).toBe(false)
  })
})
