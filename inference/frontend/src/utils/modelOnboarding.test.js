import { describe, expect, it } from 'vitest'
import {
  CHAT_MAX_OUTPUT_TOKENS_PARAMETERS,
  CHAT_TEMPERATURE_MODES,
  CHAT_THINKING_MODES,
  applyPreset,
  createModelDraft,
  deploymentPayload,
  hasMatchingChatSettings,
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
      profileCode: 'multimodal-embedding',
      chatMaxOutputTokensParameter: 'max_tokens',
      chatTemperatureMode: 'configurable',
      chatThinkingMode: 'upstream_default'
    })
    expect(CHAT_MAX_OUTPUT_TOKENS_PARAMETERS).toEqual(['max_tokens', 'max_completion_tokens'])
    expect(CHAT_TEMPERATURE_MODES).toEqual(['configurable', 'default_only'])
    expect(CHAT_THINKING_MODES).toEqual(['upstream_default', 'disabled'])
  })

  it('keeps explicit thinking choices when switching presets', () => {
    expect(applyPreset({ ...createModelDraft(), chatThinkingMode: 'disabled' }, 'chat_reasoning').chatThinkingMode).toBe('disabled')
  })

  it('preserves thinking settings through editing and payload submission', () => {
    for (const mode of CHAT_THINKING_MODES) {
      const row = { id: 'deployment', provider_connection_id: 'provider', name: 'chat', upstream_model: 'model', operations: ['chat'], modalities: ['text'], dimension: 0, chat_max_output_tokens_parameter: 'max_tokens', chat_temperature_mode: 'configurable', chat_thinking_mode: mode, status: 'active' }
      const form = deploymentPayload(row)
      const payload = deploymentPayload(form)
      expect(payload.chat_thinking_mode).toBe(mode)
      expect(payload).not.toHaveProperty('id')
      expect(form.operations).not.toBe(row.operations)
      expect(payload).toEqual(form)
    }
  })

  it('normalizes tenant allowlists', () => {
    expect(parseTenantIDs('1, 2, 1, invalid, 0')).toEqual([1, 2])
  })

  it('rejects silently reusing deployments with different Chat controls', () => {
    const draft = createModelDraft()
    const deployment = { chat_max_output_tokens_parameter: draft.chatMaxOutputTokensParameter, chat_temperature_mode: draft.chatTemperatureMode, chat_thinking_mode: draft.chatThinkingMode }
    expect(hasMatchingChatSettings(deployment, draft)).toBe(true)
    expect(hasMatchingChatSettings(deployment, { ...draft, chatThinkingMode: 'disabled' })).toBe(false)
    expect(hasMatchingChatSettings(deployment, { ...draft, chatTemperatureMode: 'default_only' })).toBe(false)
    expect(hasMatchingChatSettings(deployment, { ...draft, chatMaxOutputTokensParameter: 'max_completion_tokens' })).toBe(false)
    expect(hasMatchingChatSettings({ ...deployment, chat_thinking_mode: undefined }, draft)).toBe(false)
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
