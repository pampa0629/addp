// 宿主注入认证请求函数，组件本身不持有 Token 或业务路由。
export function createLineageApi({ request, baseUrl = '/api/v1/meta' } = {}) {
  if (typeof request !== 'function' && typeof request?.get !== 'function') {
    throw new TypeError('createLineageApi requires a host request function or client')
  }
  return {
    getGraph(params = {}) {
      const url = `${baseUrl}/lineage/graph`
      return typeof request.get === 'function' ? request.get(url, { params }) : request(url, { method: 'GET', params })
    }
  }
}

export function normalizeLineageGraph(payload) {
  return {
    subject: payload?.subject || null,
    nodes: Array.isArray(payload?.nodes) ? payload.nodes : [],
    edges: Array.isArray(payload?.edges) ? payload.edges : [],
    truncated: Boolean(payload?.truncated),
    field_lineage_status: payload?.field_lineage_status || '',
    as_of: payload?.as_of || null
  }
}

export function lineageNodeId(node) {
  if (!node) return ''
  if (node.kind === 'published_service') return `service:${node.service_id}:${node.published_revision}`
  if (node.kind === 'field_ref') return node.item_id && node.field_name && node.schema_snapshot_hash
    ? `field:${node.item_id}:${JSON.stringify(node.field_name)}:${node.schema_snapshot_hash}` : ''
  return node.item_id ? `item:${node.item_id}` : ''
}
