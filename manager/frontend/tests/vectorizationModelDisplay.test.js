import { describe, expect, it } from 'vitest'
import { buildEmbeddingModelLabels, embeddingModelLabel } from '../src/utils/vectorization.js'

describe('vectorization model display', () => {
  it('resolves a model profile to a readable profile and upstream model name', () => {
    const labels = buildEmbeddingModelLabels(
      [{ id: 'profile-1', name: 'Qwen3 VL Embedding', model_deployment_id: 'deployment-1' }],
      [{ id: 'deployment-1', upstream_model: 'qwen3-vl-embedding' }]
    )

    expect(embeddingModelLabel('profile-1', labels)).toBe('Qwen3 VL Embedding · qwen3-vl-embedding')
  })

  it('falls back to the profile name when its deployment is unavailable', () => {
    const labels = buildEmbeddingModelLabels(
      [{ id: 'profile-1', name: 'Qwen3 VL Embedding', model_deployment_id: 'deployment-1' }],
      []
    )

    expect(embeddingModelLabel('profile-1', labels)).toBe('Qwen3 VL Embedding')
  })

  it('does not expose an unknown profile UUID', () => {
    expect(embeddingModelLabel('missing-profile', new Map())).toBe('-')
  })
})
