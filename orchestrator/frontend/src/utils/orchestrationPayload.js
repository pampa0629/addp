export function buildOrchestrationPayload(source = {}) {
  return {
    name: source.name || '',
    description: source.description || '',
    steps: Array.isArray(source.steps) ? source.steps : [],
    enabled: source.enabled === true,
    schedule: source.schedule || ''
  }
}
