// In-memory API boundary for browser tests. No request reaches a running ADDP service.
export const applicationID = '38ef4190-0101-4000-8000-000000000001'
export const applicationPath = `/workbench/applications/${applicationID}`
export const runtimePath = `/data-apps/${applicationID}`
const copy = value => structuredClone(value)
const option = (value, zh, en) => ({ value, labels: { 'zh-cn': zh, en } })
const grainOptions = [option('total', '全期', 'Total'), option('month', '按月', 'Monthly')]
const directionOptions = [option('forward', '单向（主体→比较对象）', 'Forward (subject to comparison)'), option('both', '双向', 'Both directions')]

function descriptor(id, rebound) {
  const fields = [{ name: 'bucket', type: 'date' }, { name: 'value', type: 'double' }]
  if (id === 71) fields.push({ name: 'direction', type: 'string' })
  return {
    ref: { service_type: 'query', service_id: id },
    title: id === 71 ? '定向重叠率服务' : '计数服务',
    contract_fingerprint: `sha256:${String(rebound ? id + 10 : id).repeat(32)}`,
    input_contract: {
      fields: fields.map(field => ({ ...field, selectable: true, sortable: true, filterable: false, operators: [] })),
      default_selection: fields.map(field => field.name),
      named_parameters: [
        { name: 'grain', type: 'string', required: true, ...(rebound ? { options: grainOptions } : {}) },
        ...(id === 71 ? [{ name: 'directions', type: 'string', required: true, ...(rebound ? { options: directionOptions } : {}) }] : []),
      ],
      order: { stable_key: id === 71 ? ['direction', 'bucket'] : ['bucket'] },
      page: { default_limit: 50, max_limit: 100 },
    },
    output_contract: { fields, formats: ['json', 'csv'] },
    operations: [{ key: 'query', method: 'POST', path: `/api/query/metric_${id}/query`, input_kind: 'structured_query', output_kind: 'tabular' }],
  }
}

function component(id, rebound) {
  const current = descriptor(id, rebound)
  const keys = id === 71 ? ['grain', 'directions'] : ['grain']
  const fields = current.input_contract.default_selection
  return {
    id: `component-${id}`, title: id === 71 ? '双方重叠率' : '活动次数', description: 'Browser fixture',
    service_ref: current.ref, contract_fingerprint: current.contract_fingerprint,
    parameter_definitions: keys.map(key => ({ key, label: key === 'grain' ? '统计粒度' : '查询方向', control_type: 'text', required: true })),
    default_parameter_values: {},
    query_template: {
      select: fields, fixed_filter: null, parameter_filters: [],
      named_parameter_bindings: keys.map(key => ({ parameter_key: key, name: key })),
      order_by: current.input_contract.order.stable_key.map(field => ({ field, direction: 'asc' })),
      page_limit: 50, format: 'json',
    },
    renderer_type: id === 71 ? 'table' : 'chart',
    renderer_config: {
      ...(id === 71 ? { columns: fields } : { chart_type: 'bar', dimension: 'bucket', measures: ['value'] }),
      field_presentations: [
        { field: 'bucket', label: '统计期间', temporal_format: 'date' },
        { field: 'value', label: id === 71 ? '重叠率' : '次数', precision: 6 },
        ...(id === 71 ? [{ field: 'direction', label: '方向' }] : []),
      ],
    },
  }
}

export async function installMetricApplicationBackend(context, { rebound = false, locale = 'zh-cn', deferDescriptors = false, configure = () => {} } = {}) {
  const components = [component(71, rebound), component(72, rebound)]
  let draft = {
    id: applicationID, name: '指标应用回归', description: 'Isolated browser contract fixture', version: 4,
    publication_status: 'published', current_revision_number: 1, has_unpublished_changes: false,
    snapshot: {
      components,
      parameters: [
        { key: 'shared_grain', label: '统计粒度', control_type: 'text', required: true, default_value: 'total' },
        { key: 'direction', label: '查询方向', control_type: 'text', required: true, default_value: 'both' },
      ],
      parameter_bindings: components.flatMap(c => c.parameter_definitions.map(p => ({ component_id: c.id, component_parameter_key: p.key, application_parameter_key: p.key === 'grain' ? 'shared_grain' : 'direction' }))),
      parameter_presets: [], selection_bindings: [],
      page: { title: '指标应用回归', display_mode: 'desktop', refresh_interval_seconds: 0, visible_sections: ['title', 'parameters', 'query_actions'], placements: components.map((c, i) => ({ component_id: c.id, x: i * 6, y: 0, width: 6, height: 6 })) },
    },
  }
  const descriptors = { 71: descriptor(71, rebound), 72: descriptor(72, rebound) }
  configure(draft, descriptors)
  let published = { id: draft.id, name: draft.name, description: draft.description, revision_number: 1, snapshot: copy(draft.snapshot) }
  const originalPublished = copy(published)
  const requests = [], writes = [], unexpected = []
  const pending = new Map()
  await context.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await context.route(url => url.pathname.startsWith('/api/'), async route => {
    const request = route.request(), path = new URL(request.url()).pathname, method = request.method()
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
    if (path === '/api/v1/system/refresh') return send({ access_token: 'isolated-e2e-token', expires_in: 3600 })
    if (path === '/api/v1/system/users/me') return send({ id: 1, username: 'fixture-author' })
    if (path === '/api/v1/system/auth/context') return send({ context: { type: 'tenant', tenant_id: 1 }, authorization: { role_assignments: [{ permissions: ['workbench.data_application.read', 'workbench.data_application.create'] }] } })
    if (path === '/api/v1/service/consumer/services') return send({ data: Object.values(descriptors), total: 2 })
    const match = path.match(/^\/api\/v1\/service\/consumer\/services\/query\/(71|72)$/)
    if (match) {
      const id = Number(match[1])
      if (deferDescriptors) await new Promise(resolve => pending.set(id, resolve))
      return send(descriptors[id])
    }
    if (path === '/api/v1/workbench/data_applications' && method === 'POST') {
      const body = request.postDataJSON()
      writes.push({ action: 'create', body: copy(body) })
      draft = { ...copy(body), id: applicationID, version: 1, publication_status: 'unpublished', has_unpublished_changes: false }
      return send(draft, 201)
    }
    const base = `/api/v1/workbench/data_applications/${applicationID}`
    if (path === base && method === 'GET') return send(draft)
    if (path === base && method === 'PUT') {
      const body = request.postDataJSON()
      if (body.version !== draft.version) return send({ error: 'version conflict' }, 409)
      if (body.snapshot.components.some(c => c.contract_fingerprint !== descriptors[c.service_ref.service_id].contract_fingerprint)) return send({ error: 'stale contract' }, 409)
      writes.push({ action: 'save', body: copy(body) })
      draft = { ...draft, ...copy(body), version: draft.version + 1, has_unpublished_changes: true }
      return send(draft)
    }
    if (path === `${base}/publish` && method === 'POST') {
      const body = request.postDataJSON()
      if (body.version !== draft.version) return send({ error: 'version conflict' }, 409)
      writes.push({ action: 'publish', body })
      draft = { ...draft, version: draft.version + 1, current_revision_number: draft.current_revision_number + 1, has_unpublished_changes: false }
      published = { id: draft.id, name: draft.name, description: draft.description, revision_number: draft.current_revision_number, snapshot: copy(draft.snapshot) }
      return send(draft)
    }
    if (path === `${base}/runtime`) return send(published)
    const query = path.match(/^\/api\/query\/metric_(71|72)\/query$/)
    if (query) {
      const id = Number(query[1]), body = request.postDataJSON()
      requests.push({ id, body })
      const { grain, directions } = body.parameters
      if (!['total', 'month'].includes(grain) || (id === 71 && !['forward', 'both'].includes(directions))) return send({ error: 'invalid option value' }, 400)
      const months = grain === 'month' ? 12 : 1
      const rows = Array.from({ length: months }, (_, i) => ({ bucket: `2026-${String(i + 1).padStart(2, '0')}-01`, value: grain === 'total' ? 12 : 1 }))
      const data = id === 72 ? rows : (directions === 'both' ? ['forward', 'reverse'] : ['forward']).flatMap(direction => rows.map(row => ({ ...row, value: direction === 'forward' ? 0.25 : 0.5, direction })))
      return send({ data, page: { has_more: false, next_cursor: '' } })
    }
    unexpected.push({ path, method })
    return send({ error: `Unmocked browser test request: ${method} ${path}` }, 501)
  })
  return {
    descriptors, requests, writes, unexpected, originalPublished,
    get draft() { return copy(draft) },
    get published() { return copy(published) },
    // Model's own browser suite verifies the UI and versioned metric-source request.
    // Here the fixture supplies the resulting Service Descriptor boundary.
    rebindServices() { for (const id of [71, 72]) descriptors[id] = descriptor(id, true) },
    pending,
    releaseDescriptor(id) { const release = pending.get(id); pending.delete(id); release?.() },
    releaseAll() { deferDescriptors = false; for (const release of pending.values()) release(); pending.clear() },
  }
}
