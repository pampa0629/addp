/** Canonical System-managed host filters; management intent is not runtime health. */
export const hostNodeKinds = Object.freeze(['physical', 'virtual'])
export function resolveHostNodeFilters(query = {}) {
  const nodeKind = typeof query.node_kind === 'string' && hostNodeKinds.includes(query.node_kind) ? query.node_kind : ''
  const enabled = typeof query.enabled === 'string' && ['true', 'false'].includes(query.enabled) ? query.enabled : ''
  return { nodeKind, enabled, query: { ...(nodeKind ? { node_kind: nodeKind } : {}), ...(enabled ? { enabled } : {}) } }
}
