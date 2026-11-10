const id = /^[0-9a-f]{32}$/
const spanID = /^[0-9a-f]{16}$/
const moduleName = /^[a-z][a-z0-9_-]{0,49}$/
const instanceName = /^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$/
const phaseKinds = { enqueue: 'producer', claim: 'consumer', dispatch: 'producer', terminal: 'internal' }
const canonical = value => typeof value === 'string' && id.test(value) && !/^0+$/.test(value)
export { canonical as isTraceID }
export const traceRecentMinutes = Object.freeze({ '30m': 30, '1h': 60 })
const httpMethods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS', 'CONNECT', 'TRACE', 'QUERY', '_OTHER']
export const isTraceHTTPRoute = route => typeof route === 'string' && route.startsWith('/') &&
  new TextEncoder().encode(route).length <= 1024 && !/[\x00-\x1f\x7f-\x9f?#]/.test(route)
export function traceRoute(query = {}) {
  const invalid = () => { throw new Error('invalid_trace_query') }
  const allowed = ['instance_id', 'trace_id', 'start', 'end', 'range', 'module_name', 'role', 'page', 'http_method', 'http_route']
  if (Object.entries(query).some(([key, value]) => !allowed.includes(key) || typeof value !== 'string' || !value)) invalid()
  if (query.instance_id && (!/^[1-9]\d*$/.test(query.instance_id) || !Number.isSafeInteger(Number(query.instance_id)))) invalid()
  if (query.trace_id && !canonical(query.trace_id)) invalid()
  if (query.module_name && !/^[a-z][a-z0-9_-]{0,49}$/.test(query.module_name)) invalid()
  if (query.role && !['backend', 'ingress'].includes(query.role)) invalid()
  if (query.instance_id && query.page && query.page !== '1') invalid()
  if (query.page && (!/^[1-9]\d*$/.test(query.page) || !Number.isSafeInteger(Number(query.page)))) invalid()
  if (query.range && (!Object.hasOwn(traceRecentMinutes, query.range) || query.start || query.end)) invalid()
  if ((query.http_method || query.http_route) && (!query.instance_id || !httpMethods.includes(query.http_method) || !isTraceHTTPRoute(query.http_route))) invalid()
  if (query.start || query.end) {
    const start = Date.parse(query.start), end = Date.parse(query.end)
    if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end || end - start > 3600000) invalid()
  }
  return { query: { ...query }, page: Number(query.page || 1), instanceID: Number(query.instance_id || 0), role: query.role || 'backend', module: query.module_name || '' }
}

export function findHTTPFailureSpan(trace, instance, query, window) {
  const start = Date.parse(window.start), end = Date.parse(window.end)
  return trace.spans.find(span => span.scope === 'addp.http' && span.kind === 'server' &&
    span.module_name === instance.module_name && span.instance_id === instance.instance_id && span.role === instance.role &&
    span.method === query.http_method && span.route === query.http_route && span.status >= 500 && span.status <= 599 &&
    Date.parse(span.end) >= start && Date.parse(span.end) < end)
}
export function validateTraces(value, selected, requestedID = '') {
  const invalid = () => { throw new Error('invalid_trace_response') }
  const date = v => typeof v === 'string' && Number.isFinite(Date.parse(v))
  if (!value || !Array.isArray(value.data) || value.data.length > 100 || value.coverage !== 'sampled' || value.policy_version !== 2 || !date(value.start) || !date(value.end) || !date(value.queried_at) || Date.parse(value.start) >= Date.parse(value.end) || Date.parse(value.end) > Date.parse(value.queried_at) || Date.parse(value.end) - Date.parse(value.start) > 3600000) invalid()
  let count = 0
  const seen = new Set()
  for (const trace of value.data) {
    if (!canonical(trace.trace_id) || seen.has(trace.trace_id) || (requestedID && trace.trace_id !== requestedID) || trace.coverage !== 'sampled' || trace.policy_version !== 2 || !Array.isArray(trace.spans) || !trace.spans.length || !Array.isArray(trace.edges) || typeof trace.partial !== 'boolean' || !Number.isFinite(trace.duration_ms) || trace.duration_ms < 0 || !date(trace.start) || !date(trace.end)) invalid()
    seen.add(trace.trace_id)
    const spans = new Map()
    let selectedFound = false
    for (const span of trace.spans) {
      if (!spanID.test(span.span_id) || /^0+$/.test(span.span_id) || spans.has(span.span_id) || (span.parent_span_id && (!spanID.test(span.parent_span_id) || /^0+$/.test(span.parent_span_id))) || !['backend', 'ingress'].includes(span.role) || typeof span.module_name !== 'string' || !/^[a-z][a-z0-9_-]{0,49}$/.test(span.module_name) || typeof span.instance_id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$/.test(span.instance_id) || typeof span.route !== 'string' || span.route.length > 1024 || /[\r\n?#]/.test(span.route) || !Number.isInteger(span.status) || span.status < 0 || (span.status > 0 && span.status < 100) || span.status > 599 || typeof span.failed !== 'boolean' || !date(span.start) || !date(span.end) || Date.parse(span.start) > Date.parse(span.end) || !Number.isFinite(span.duration_ms) || span.duration_ms < 0) invalid()
      if (!Array.isArray(span.links) || span.links.length > 2 || !Number.isSafeInteger(span.attempt) || span.attempt < 0) invalid()
      if (span.scope === 'addp.http') {
        if (!['client', 'server'].includes(span.kind) || !httpMethods.includes(span.method) || span.phase || span.outcome || span.attempt !== 0 || span.links.length) invalid()
      } else if (span.scope === 'addp.execution') {
        if (!Object.hasOwn(phaseKinds, span.phase) || phaseKinds[span.phase] !== span.kind || span.parent_span_id || span.role !== 'backend' || span.method !== '' || span.route !== '' || span.status !== 0 || (span.phase === 'enqueue' ? span.attempt !== 0 : span.attempt < 1) || !['success','failed','timeout','cancelled','rejected'].includes(span.outcome) || span.failed !== ['failed','timeout'].includes(span.outcome)) invalid()
      } else invalid()
      const linked = new Set()
      for (const link of span.links) {
        if (!link || Object.keys(link).sort().join(',') !== 'instance_id,module_name,role,runtime_instance_id,span_id,started_at,trace_id' || !canonical(link.trace_id) || link.trace_id === trace.trace_id || typeof link.span_id !== 'string' || !spanID.test(link.span_id) || /^0+$/.test(link.span_id) || typeof link.module_name !== 'string' || !moduleName.test(link.module_name) || typeof link.instance_id !== 'string' || !instanceName.test(link.instance_id) || !['backend','ingress'].includes(link.role) || !date(link.started_at) || !link.started_at.endsWith('Z') || !Number.isSafeInteger(link.runtime_instance_id) || link.runtime_instance_id < 1 || linked.has(link.trace_id + '/' + link.span_id)) invalid()
        linked.add(link.trace_id + '/' + link.span_id)
      }
      spans.set(span.span_id, span)
      selectedFound ||= !selected || (span.instance_id === selected.instance_id && span.module_name === selected.module_name)
    }
    if (!selectedFound) invalid()
    for (const edge of trace.edges) {
      const client = spans.get(edge.client_span_id), server = spans.get(edge.server_span_id)
      if (!client || !server || client.kind !== 'client' || server.kind !== 'server' || server.parent_span_id !== client.span_id || edge.from_module !== client.module_name || edge.from_instance !== client.instance_id || edge.to_module !== server.module_name || edge.to_instance !== server.instance_id) invalid()
    }
    count += trace.spans.length
    if (count > 1000) invalid()
  }
  if (requestedID && value.data.length !== 1) invalid()
  return value
}

// Follow one authorized causal target with its own small time window. The source
// trace may be hours newer; it must not constrain the target lookup window.
export function traceLinkQuery(link, now = Date.now()) {
  const started = Date.parse(link.started_at), oldest = now - 72 * 3600000
  if (!Number.isFinite(started) || started > now || started < oldest || !Number.isSafeInteger(link.runtime_instance_id) || link.runtime_instance_id < 1 || !canonical(link.trace_id) || typeof link.module_name !== 'string' || !moduleName.test(link.module_name) || !['backend','ingress'].includes(link.role)) throw new Error('invalid_trace_query')
  const start = Math.max(oldest, started - 1000), end = Math.min(now, started + 1000)
  if (start >= end) throw new Error('invalid_trace_query')
  return { instance_id: String(link.runtime_instance_id), module_name: link.module_name, role: link.role, trace_id: link.trace_id, start: new Date(start).toISOString(), end: new Date(end).toISOString() }
}

export function traceTimeline(trace) {
  const spans = [...trace.spans].sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
  const byID = new Map(spans.map(span => [span.span_id, span]))
  const children = new Map()
  for (const span of spans) {
    const siblings = children.get(span.parent_span_id) || []
    siblings.push(span)
    children.set(span.parent_span_id, siblings)
  }
  const start = Date.parse(trace.start)
  const duration = Math.max(1, trace.duration_ms, Date.parse(trace.end) - start)
  const visited = new Set()
  const rows = []
  function append(span, depth) {
    if (visited.has(span.span_id)) return
    visited.add(span.span_id)
    const offset = Math.min(duration, Math.max(0, Date.parse(span.start) - start))
    rows.push({
      span, depth,
      offsetPercent: offset / duration * 100,
      widthPercent: Math.min(span.duration_ms, duration - offset) / duration * 100
    })
    for (const child of children.get(span.span_id) || []) append(child, depth + 1)
  }
  for (const span of spans) {
    if (!byID.has(span.parent_span_id)) append(span, 0)
  }
  for (const span of spans) append(span, 0)

  const entry = spans.find(span => span.kind === 'server' && !span.parent_span_id)
  const entryCall = trace.edges.find(edge => entry && byID.get(edge.client_span_id)?.parent_span_id === entry.span_id)
  const operation = entry?.role === 'ingress' && entryCall ? byID.get(entryCall.server_span_id) : entry
  let longestDownstream
  for (const edge of trace.edges) {
    if (edge.from_module === edge.to_module) continue
    const server = byID.get(edge.server_span_id)
    if (!longestDownstream || server.duration_ms > longestDownstream.duration_ms) longestDownstream = server
  }
  const failures = rows.filter(({ span }) => span.failed)
  const firstFailure = [...failures].sort((a, b) => b.depth - a.depth)
    .find(({ span }) => span.kind !== 'client') || failures[0]
  return { rows, entry, operation: operation || spans[0], longestDownstream, failures, firstFailure }
}
