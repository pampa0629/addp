import { resolveCanonicalTabRouteState } from '@common-ui/utils/recoverableRouteState'

const KNOWLEDGE_SERVICE_TABS = ['config', 'docs', 'test']

function queryValue(value) {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

export function resolveKnowledgeServiceRouteState({ routeQuery = {}, graphs = [] }) {
  const rawGraphId = queryValue(routeQuery.graph_id)
  const requestedGraphId = Number(rawGraphId)
  const graph = rawGraphId && Number.isInteger(requestedGraphId)
    ? graphs.find(item => item.id === requestedGraphId)
    : null
  const routeState = resolveCanonicalTabRouteState({
    allowedTabs: KNOWLEDGE_SERVICE_TABS,
    defaultTab: 'config',
    routeQuery,
    preservedQuery: graph ? { graph_id: String(graph.id) } : {}
  })

  return { graphId: graph?.id ?? null, ...routeState }
}
