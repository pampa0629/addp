export function buildOrchestrationPayload(source = {}) {
  return {
    name: source.name || '',
    description: source.description || '',
    steps: Array.isArray(source.steps) ? source.steps : [],
    editor_layout: source.editor_layout || {},
    enabled: source.enabled === true,
    schedule: source.schedule || ''
  }
}
