export async function mockGraph(page, { permissions = ['graph.ontology.read'], responses = {} } = {}) {
  const unexpected = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/system/refresh' && route.request().method() === 'POST') return route.fulfill({ json: { access_token: 'graph-fixture-token', expires_in: 300 } })
    if (route.request().method() !== 'GET') {
      unexpected.push(`${route.request().method()} ${path}`)
      return route.fulfill({ status: 500, json: { error: `unexpected write ${path}` } })
    }
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: 42, username: 'graph-reader' } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      principal: { type: 'user', id: '42' }, context: { type: 'tenant', tenant_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '7' }, permissions }] }
    } })
    if (Object.hasOwn(responses, path)) return route.fulfill({ json: responses[path] })
    const ontology = { id: 1, name: '导航测试本体', status: 'active',
      entity_types: [{ id: 11, name: 'Node', label: 'Node', properties: [] }], relation_types: [] }
    if (path === '/api/v1/graph/ontologies') return route.fulfill({ json: [ontology] })
    if (path === '/api/v1/graph/ontologies/1') return route.fulfill({ json: ontology })
    if (path === '/api/v1/graph/ontologies/1/versions') return route.fulfill({ json: [] })
    unexpected.push(`${route.request().method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: `unexpected ${path}` } })
  })
  return unexpected
}
