import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import axios from 'axios'

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
const withoutImports = source => source.replace(/^import\s+[\s\S]*?\s+from\s+['"][^'"]+['"];?\s*$/gm, '')

function harness({ token = 'opaque-user', failureStatus } = {}) {
  const store = { token }
  // Exercise the shared client and its real Bearer interceptor with an in-memory transport.
  const sharedSource = withoutImports(read('../../../common-frontend/basic/src/composables/useAuth.js'))
    .replace(/^export /gm, '').replace(/import\.meta\.env/g, '({})')
  const createSharedClient = new Function('axios', 'getAccessToken', `${sharedSource}\nreturn createAPIClient`)(axios, () => store.token)
  const requests = []
  const createAPIClient = (...args) => {
    const client = createSharedClient(...args)
    client.defaults.adapter = async config => {
      requests.push({
        method: config.method, url: axios.getUri(config),
        authorization: config.headers.get('Authorization'), apiKey: config.headers.get('X-API-Key'),
        body: config.data ? JSON.parse(config.data) : undefined
      })
      const response = { data: { rows_count: 0, rows: [] }, status: 200, statusText: 'OK', headers: {}, config }
      if (failureStatus) {
        response.status = failureStatus
        response.data = { error: 'execution refused' }
        throw new axios.AxiosError('execution refused', undefined, config, undefined, response)
      }
      return response
    }
    return client
  }
  const loadClient = name => new Function('createAPIClient', 'useAuthStore',
    withoutImports(read(`../src/api/${name}.js`)).replace(/^export default/gm, 'return')
  )(createAPIClient, () => store)
  const client = loadClient('client')
  const queryExecutionClient = loadClient('queryExecutionClient')
  const clients = { './client': client, './queryExecutionClient': queryExecutionClient }
  const bindings = []
  const dependencies = []
  const apiSource = read('../src/api/graphQueryService.js').replace(
    /^import (\w+) from ['"]([^'"]+)['"]\s*$/gm,
    (_, binding, path) => {
      assert.ok(Object.hasOwn(clients, path), `unexpected API dependency: ${path}`)
      bindings.push(binding)
      dependencies.push(clients[path])
      return ''
    }
  ).replace(/^export default/gm, 'return')
  const api = new Function(...bindings, apiSource)(...dependencies)
  return { api, requests }
}

test('graph execution uses the data-plane client with the exact body and current User Bearer', async () => {
  const { api, requests } = harness()
  const body = { parameters: { city: '北京', minimum: 12, enabled: true }, page: 2, page_size: 5 }
  const result = await api.executeQuery('city_graph', body)
  assert.deepEqual(result, { rows_count: 0, rows: [] })
  assert.deepEqual(requests, [{
    method: 'post', url: '/api/gquery/city_graph', authorization: 'Bearer opaque-user',
    apiKey: undefined, body
  }])
})

test('graph management keeps its control-plane path and execution without a token stays anonymous', async () => {
  const { api, requests } = harness({ token: null })
  await api.getService(7)
  await api.executeQuery('public_graph', { parameters: {}, page: 1, page_size: 20 })
  assert.deepEqual(requests.map(request => request.url), ['/api/v1/service/graph/7', '/api/gquery/public_graph'])
  assert.ok(requests.every(request => !request.authorization && !request.apiKey))
})

test('graph execution propagates authorization and protection failures without another route or retry', async () => {
  for (const status of [403, 500]) {
    const { api, requests } = harness({ failureStatus: status })
    await assert.rejects(api.executeQuery('protected_graph', { parameters: {} }), error => {
      assert.equal(error.response.status, status)
      assert.equal(error.response.data.error, 'execution refused')
      return true
    })
    assert.equal(requests.length, 1)
    assert.equal(requests[0].url, '/api/gquery/protected_graph')
  }
})
