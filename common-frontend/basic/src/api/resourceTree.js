import axios from 'axios'
import { getAccessToken } from '../auth/authSession.js'
import { getEngineFamily } from '../utils/engineDisplay.js'

const createAuthenticatedAxios = () => {
  const instance = axios.create()
  instance.interceptors.request.use((config) => {
    const token = getAccessToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  })
  return instance
}

const http = createAuthenticatedAxios()

const unwrap = (response) => response?.data?.data || response?.data

export async function listResourceTreeEngines(apiBaseUrl, options = {}) {
  const response = await http.get(`${apiBaseUrl}/engines`)
  let engines = unwrap(response) || []
  const engineFamilies = options.engineFamilies || []
  engines = engines.map(engine => ({
    ...engine,
    id: engine.id || engine.engine_id,
    name: engine.name || engine.resource_name,
    engine_type: engine.engine_type || engine.resource_type
  }))
  if (engineFamilies.length > 0) {
    engines = engines.filter(engine => engineFamilies.includes(getEngineFamily(engine)))
  }
  if (typeof options.engineFilter === 'function') {
    engines = engines.filter(options.engineFilter)
  }
  return engines
}

export async function getResourceTree(apiBaseUrl, engineId, options = {}) {
  const response = await http.get(`${apiBaseUrl}/resource-tree/${engineId}`, {
    params: { expand_depth: options.expandDepth ?? 1 }
  })
  return unwrap(response)
}

export async function getResourceTreeNode(apiBaseUrl, engineId, locator) {
  const response = await http.get(`${apiBaseUrl}/resource-tree/${engineId}/node`, {
    params: { locator }
  })
  return unwrap(response)
}

export async function getResourceTreeAncestors(apiBaseUrl, engineId, locator) {
  const response = await http.get(`${apiBaseUrl}/resource-tree/${engineId}/ancestors`, {
    params: { locator }
  })
  return unwrap(response)
}

export async function searchResourceTree(apiBaseUrl, engineId, keyword, options = {}) {
  const params = {
    q: keyword,
    limit: options.limit ?? 50
  }
  if (options.nodeTypes) {
    params.node_types = Array.isArray(options.nodeTypes) ? options.nodeTypes.join(',') : options.nodeTypes
  }
  const response = await http.get(`${apiBaseUrl}/resource-tree/${engineId}/search`, { params })
  return unwrap(response)
}

export async function refreshResourceTreeNode(apiBaseUrl, engineId, locator) {
  const response = await http.post(`${apiBaseUrl}/resource-tree/${engineId}/refresh`, null, {
    params: { locator }
  })
  return unwrap(response)
}
