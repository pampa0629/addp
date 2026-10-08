export const targetKinds = { host_resources: 'node_exporter', container_resources: 'cadvisor' }
export function isTargetUUID(value) {
  return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u.test(value) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/u.test(value)
}
export function resolveTargetRoute(query = {}, creating = false) {
  const integer = (value, max, fallback) => typeof value === 'string' && /^[1-9][0-9]*$/u.test(value) && Number(value) <= max ? Number(value) : fallback
  const page = integer(query.page, 1000000, 1)
  const pageSize = integer(query.page_size, 100, 20)
  const canonical = { ...(creating && isTargetUUID(query.node_id) ? { node_id: query.node_id } : {}), ...(page === 1 ? {} : { page: String(page) }), ...(pageSize === 20 ? {} : { page_size: String(pageSize) }) }
  return { page, pageSize, query: canonical, changed: Object.keys(query).length !== Object.keys(canonical).length || Object.keys(canonical).some(key => query[key] !== canonical[key]) }
}
export function targetInput(form, target) {
  const kind = target ? target.monitor_kind : form.monitor_kind
  const node = target ? target.subject.node_id : form.node_id.trim()
  if (!isTargetUUID(node) || !targetKinds[kind] || !form.endpoint.trim() || form.endpoint.trim().length > 512) return null
  return { ...(target ? { version: target.version } : {}), subject: { kind: 'node', node_id: node }, monitor_kind: kind,
    source: { type: targetKinds[kind], endpoint: form.endpoint.trim() }, enabled: form.enabled }
}
