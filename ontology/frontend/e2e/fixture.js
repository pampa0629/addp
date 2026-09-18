// Controlled HTTP fixture. All /api requests are intercepted; no developer services or data are used.
export const root = '/ontology/ontologies'
export const editor = `${root}/beijing_outdoor/revisions/1`
export const allPermissions = [
  'ontology.revision.read',
  'ontology.revision.update',
  'ontology.revision.publish',
  'system.execution_authorization.create'
]
export async function installBackend(context, options = {}) {
  const definition = {
    classes: [{ id: 'activity', name: '北京户外活动', parents: [] }],
    properties: [],
    relations: [],
    rules: []
  }
  let revision = {
    ontology_id: 'beijing_outdoor',
    revision: 1,
    version: 1,
    status: options.status || 'draft',
    snapshot: {
      definition: {
        scope: { tenant_id: 7, ontology_id: 'beijing_outdoor', revision: 1 },
        ...definition
      }
    },
    digest: 'digest',
    initial_generation: null,
    initial_execution_id: null,
    updated_at: '2026-09-18T08:00:00Z'
  }
  let head = {
    ontology_id: 'beijing_outdoor',
    last_revision: 1,
    activation_version: 0,
    active_revision: null,
    active_generation: null
  }
  let projection = null
  if (['published', 'withdrawn'].includes(revision.status)) {
    revision.initial_generation = 'g1'
    revision.initial_execution_id = 'e1'
    projection = {
      ontology_id: head.ontology_id,
      revision: 1,
      generation: 'g1',
      execution_id: 'e1',
      status: options.projection || 'failed',
      baseline_version: 0
    }
  }
  const state = {
    writes: [],
    unexpected: [],
    failWrite: null,
    sessionExpired: false,
    headers: [],
    get revision() {
      return revision
    },
    get head() {
      return head
    },
    get projection() {
      return projection
    },
    activate() {
      projection.status = 'ready'
      head.active_revision = revision.revision
      head.active_generation = projection.generation
      head.activation_version++
    }
  }
  await context.addInitScript(
    (locale) => localStorage.setItem('addp-lang', locale),
    options.locale || 'zh-cn'
  )
  await context.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const req = route.request(),
        url = new URL(req.url()),
        path = url.pathname,
        method = req.method()
      const send = (body, status = 200) =>
        route.fulfill({
          status,
          contentType: 'application/json',
          body: JSON.stringify(body)
        })
      if (path === '/api/v1/system/refresh')
        return state.sessionExpired
          ? send({ error: 'expired' }, 401)
          : send({
              access_token: 'ontology-isolated-token',
              expires_in: 3600
            })
      if (path === '/api/v1/system/users/me')
        return send({ id: 7, username: 'ontology-fixture' })
      if (path === '/api/v1/system/auth/context')
        return send({
          context: { type: options.contextType || 'tenant', tenant_id: 7 },
          authorization: {
            role_assignments: [
              { permissions: options.permissions || allPermissions }
            ]
          }
        })
      state.headers.push(req.headers()['accept-language'])
      const base = '/api/v1/ontology/ontologies',
        own = `${base}/beijing_outdoor`
      if (state.sessionExpired) return send({ error: 'expired' }, 401)
      if (method === 'GET') {
        if (path === base)
          return send({
            data: options.empty ? [] : [head],
            total: options.empty ? 0 : 1,
            page: Number(url.searchParams.get('page')),
            page_size: 20,
            total_pages: 1
          })
        if (path === own) return send(head)
        if (path === `${own}/revisions`)
          return send({
            data: [revision],
            total: 1,
            page: 1,
            page_size: 20,
            total_pages: 1
          })
        if (path === `${own}/revisions/${revision.revision}`)
          return send(revision)
        if (path === `${own}/revisions/${revision.revision}/projection`)
          return projection
            ? send(projection)
            : send({ error: 'not found' }, 404)
      } else {
        const body = req.postDataJSON()
        state.writes.push({ path, method, body })
        if (state.failWrite && state.failWrite !== 'committed-admission') {
          const fail = state.failWrite
          state.failWrite = null
          if (fail === 'network') return route.abort('failed')
          return send(
            {
              error: fail === 409 ? 'version conflict' : 'admission failed',
              error_code:
                fail === 409
                  ? 'resource_version_conflict'
                  : 'projection_admission_failed'
            },
            fail
          )
        }
        if (path === `${own}/revisions` && method === 'POST') {
          revision = {
            ...revision,
            revision: body.revision,
            version: 1,
            status: 'draft',
            snapshot: { definition: body.definition },
            initial_generation: null
          }
          head.last_revision = body.revision
          return send(revision, 201)
        }
        if (
          path === `${own}/revisions/${revision.revision}` &&
          method === 'PUT'
        ) {
          revision = {
            ...revision,
            version: revision.version + 1,
            snapshot: { definition: body.definition }
          }
          return send(revision)
        }
        const action = path.split('/').at(-1)
        if (['submit', 'return', 'withdraw'].includes(action)) {
          revision.status = {
            submit: 'in_review',
            return: 'draft',
            withdraw: 'withdrawn'
          }[action]
          revision.version++
          if (action === 'withdraw') {
            head.active_revision = null
            head.active_generation = null
          }
          return send(revision)
        }
        if (action === 'publish' || action === 'rebuild') {
          revision.status = 'published'
          revision.version++
          revision.initial_generation ||= 'g1'
          projection = {
            ontology_id: head.ontology_id,
            revision: revision.revision,
            generation: action === 'rebuild' ? 'g2' : 'g1',
            status: 'pending',
            execution_id: 'e1',
            baseline_version: head.activation_version
          }
          if (state.failWrite === 'committed-admission') {
            state.failWrite = null
            projection.status = 'failed'
            return send(
              {
                error: 'admission failed',
                error_code: 'projection_admission_failed',
                intent: {
                  ontology_id: head.ontology_id,
                  revision: revision.revision,
                  version: revision.version,
                  generation: projection.generation,
                  execution_id: 'e1'
                }
              },
              502
            )
          }
          return send(
            {
              ontology_id: head.ontology_id,
              revision: revision.revision,
              version: revision.version,
              generation: projection.generation,
              execution_id: 'e1'
            },
            202
          )
        }
      }
      state.unexpected.push(`${method} ${path}`)
      return send({ error: 'unexpected fixture request' }, 500)
    }
  )
  return state
}
