import { lineageNodeId } from './lineageApi.js'

export const FIELD_HEADER_HEIGHT = 64
export const FIELD_ROW_HEIGHT = 34
export const FIELD_CARD_WIDTH = 224
export const FIELD_FONT_SIZE = 18

// Group only the same item and frozen schema; field identity remains untouched.
export function projectLineageFields(nodes, edges) {
  const groups = new Map()
  const endpoints = new Map()
  for (const node of nodes) {
    const fieldId = lineageNodeId(node)
    if (node.kind !== 'field_ref' || !fieldId || endpoints.has(fieldId)) continue
    const id = `table:${node.item_id}:${node.schema_snapshot_hash}`
    if (!groups.has(id)) groups.set(id, { id, node, fields: [] })
    const group = groups.get(id)
    endpoints.set(fieldId, { id, index: group.fields.length })
    group.fields.push(node)
  }
  return {
    nodes: [...groups.values()].map(group => {
      const height = FIELD_HEADER_HEIGHT + group.fields.length * FIELD_ROW_HEIGHT + 8
      return { ...group, size: [FIELD_CARD_WIDTH, height], anchors: group.fields.flatMap((_, index) => {
        const y = (FIELD_HEADER_HEIGHT + (index + 0.5) * FIELD_ROW_HEIGHT) / height
        return [[0, y], [1, y]]
      }) }
    }),
    edges: edges.flatMap((edge, index) => {
      const source = endpoints.get(lineageNodeId(edge.source))
      const target = endpoints.get(lineageNodeId(edge.target))
      return source && target ? [{ id: `lineage-edge:${index}`, source: source.id, target: target.id, sourceAnchor: source.index * 2 + 1, targetAnchor: target.index * 2, _edge: edge }] : []
    })
  }
}

// A field focus follows each direction independently, excluding sibling outputs.
export function lineageFieldConnections(edges, fieldId) {
  const fields = new Set([fieldId])
  const connections = new Set()
  for (const [from, to] of [['target', 'source'], ['source', 'target']]) {
    const seen = new Set([fieldId])
    const frontier = [fieldId]
    for (let cursor = 0; cursor < frontier.length; cursor++) {
      edges.forEach((edge, index) => {
        if (lineageNodeId(edge[from]) !== frontier[cursor]) return
        connections.add(`lineage-edge:${index}`)
        const next = lineageNodeId(edge[to])
        fields.add(next)
        if (!seen.has(next)) { seen.add(next); frontier.push(next) }
      })
    }
  }
  return { fields, connections }
}
