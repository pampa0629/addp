const id = /^[0-9a-f]{32}$/
const spanID = /^[0-9a-f]{16}$/
const canonical = value => typeof value === 'string' && id.test(value) && !/^0+$/.test(value)
export function traceRoute(query = {}) {
  const invalid = () => { throw new Error('invalid_trace_query') }
  const allowed = ['instance_id', 'trace_id', 'start', 'end', 'module_name', 'role', 'page']
  if (Object.entries(query).some(([key, value]) => !allowed.includes(key) || typeof value !== 'string' || !value)) invalid()
  if (query.instance_id && (!/^[1-9]\d*$/.test(query.instance_id) || !Number.isSafeInteger(Number(query.instance_id)))) invalid()
  if (query.trace_id && (!query.instance_id || !canonical(query.trace_id))) invalid()
  if (query.module_name && !/^[a-z][a-z0-9_-]{0,49}$/.test(query.module_name)) invalid()
  if (query.role && !['backend', 'ingress'].includes(query.role)) invalid()
  if (query.instance_id && query.page && query.page !== '1') invalid()
  if (query.page && (!/^[1-9]\d*$/.test(query.page) || !Number.isSafeInteger(Number(query.page)))) invalid()
  if (query.start || query.end) {
    const start = Date.parse(query.start), end = Date.parse(query.end)
    if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end || end - start > 3600000) invalid()
  }
  return { query: { ...query }, page: Number(query.page || 1), instanceID: Number(query.instance_id || 0), role: query.role || 'backend', module: query.module_name || '' }
}
export function validateTraces(value, selected, requestedID = '') {
  const invalid = () => { throw new Error('invalid_trace_response') }
  const date = v => typeof v === 'string' && Number.isFinite(Date.parse(v))
  if (!value || !Array.isArray(value.data) || value.data.length > 100 || value.coverage !== 'sampled' || value.policy_version !== 1 || !date(value.start) || !date(value.end) || !date(value.queried_at) || Date.parse(value.start) >= Date.parse(value.end) || Date.parse(value.end) > Date.parse(value.queried_at) || Date.parse(value.end) - Date.parse(value.start) > 3600000) invalid()
  let count = 0
  const seen = new Set()
  for (const trace of value.data) {
    if (!canonical(trace.trace_id) || seen.has(trace.trace_id) || (requestedID && trace.trace_id !== requestedID) || trace.coverage !== 'sampled' || trace.policy_version !== 1 || !Array.isArray(trace.spans) || !trace.spans.length || !Array.isArray(trace.edges) || typeof trace.partial !== 'boolean' || !Number.isFinite(trace.duration_ms) || trace.duration_ms < 0 || !date(trace.start) || !date(trace.end)) invalid()
    seen.add(trace.trace_id)
    const spans = new Map()
    let selectedFound = false
    for (const span of trace.spans) {
      if (!spanID.test(span.span_id) || /^0+$/.test(span.span_id) || spans.has(span.span_id) || (span.parent_span_id && (!spanID.test(span.parent_span_id) || /^0+$/.test(span.parent_span_id))) || !['client', 'server'].includes(span.kind) || !['backend', 'ingress'].includes(span.role) || typeof span.module_name !== 'string' || !/^[a-z][a-z0-9_-]{0,49}$/.test(span.module_name) || typeof span.instance_id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$/.test(span.instance_id) || !['GET','POST','PUT','DELETE','PATCH','HEAD','OPTIONS','CONNECT','TRACE','QUERY','_OTHER'].includes(span.method) || typeof span.route !== 'string' || span.route.length > 1024 || /[\r\n?#]/.test(span.route) || !Number.isInteger(span.status) || span.status < 0 || (span.status > 0 && span.status < 100) || span.status > 599 || typeof span.failed !== 'boolean' || !date(span.start) || !date(span.end) || Date.parse(span.start) > Date.parse(span.end) || !Number.isFinite(span.duration_ms) || span.duration_ms < 0) invalid()
      spans.set(span.span_id, span)
      selectedFound ||= span.instance_id === selected.instance_id && span.module_name === selected.module_name
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
