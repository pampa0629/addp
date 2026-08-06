export const CAPABILITY_PRESETS = {
  chat_default: {
    operations: ['chat'],
    modalities: ['text'],
    dimension: 0,
    profileCode: 'chat-default'
  },
  chat_reasoning: {
    operations: ['chat'],
    modalities: ['text'],
    dimension: 0,
    profileCode: 'chat-reasoning'
  },
  text_embedding: {
    operations: ['embedding'],
    modalities: ['text'],
    dimension: 1536,
    profileCode: 'text-embedding'
  },
  multimodal_embedding_2560: {
    operations: ['embedding'],
    modalities: ['text', 'image'],
    dimension: 2560,
    profileCode: 'multimodal-embedding'
  },
  rerank: {
    operations: ['rerank'],
    modalities: ['text'],
    dimension: 0,
    profileCode: 'rerank-default'
  }
}

export function capabilityPreset(code) {
  return CAPABILITY_PRESETS[code] || CAPABILITY_PRESETS.chat_default
}

export function createModelDraft(presetCode = 'chat_default') {
  const preset = capabilityPreset(presetCode)
  return {
    upstreamModel: '',
    preset: presetCode,
    dimension: preset.dimension,
    profileCode: preset.profileCode
  }
}

export function applyPreset(draft, presetCode) {
  const preset = capabilityPreset(presetCode)
  return {
    ...draft,
    preset: presetCode,
    dimension: preset.dimension,
    profileCode: preset.profileCode
  }
}

export function parseTenantIDs(value) {
  if (!value?.trim()) return []
  return [...new Set(value.split(',')
    .map(item => Number(item.trim()))
    .filter(item => Number.isInteger(item) && item > 0))]
}

export function modelOptions(discovered = [], suggested = []) {
  const values = new Set()
  for (const item of discovered) {
    const id = String(item?.id || '').trim()
    if (id) values.add(id)
  }
  for (const item of suggested) {
    const id = String(item?.upstream_model || '').trim()
    if (id) values.add(id)
  }
  return [...values].sort((left, right) => left.localeCompare(right))
}

export function isValidProfileCode(value) {
  return /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(String(value || '').trim())
}
