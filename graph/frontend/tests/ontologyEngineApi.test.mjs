import { beforeEach, describe, expect, it, vi } from 'vitest'
import client from '../src/api/client'
import { engineAPI, ontologyAPI } from '../src/api/ontology'
vi.mock('../src/api/client', () => ({ default: { get: vi.fn(), post: vi.fn() } }))
describe('business graph target API', () => {
  beforeEach(() => { client.get.mockReset(); client.post.mockReset() })
  it('lists both supported business engines through the narrow catalog API', async () => {
    client.get.mockImplementation((_, { params }) => Promise.resolve([{ id: params.engine_type === 'neo4j' ? 7 : 8, engine_type: params.engine_type }]))
    expect((await engineAPI.getGraphEngines()).map(engine => engine.engine_type)).toEqual(['neo4j', 'falkordb'])
    expect(client.get.mock.calls.every(([path]) => path === '/system/engine-catalog/engines')).toBe(true)
  })
  it('preserves native graph paths and traverses namespace branches without guessing names', async () => {
    const graph = { role: 'leaf', kind: 'graph', name: '中文.a/b', path: { version: 'catalog.path/v1', engine_id: 8, segments: [{ term: 'server', kind: 'server', name: '' }, { term: 'graph', kind: 'graph', name: '中文.a/b' }] } }
    const branch = { role: 'branch', path: { segments: [{ term: 'server', kind: 'server', name: '' }, { term: 'database', kind: 'namespace', name: 'business' }] } }
    client.post.mockResolvedValueOnce({ nodes: [graph, branch] }).mockResolvedValueOnce({ nodes: [{ ...graph, name: 'graph' }] })
    const targets = await engineAPI.getGraphTargets(8)
    expect(targets[0].path).toEqual(graph.path)
    expect(client.post.mock.calls[1][1].path).toEqual(branch.path)
  })
  it.each(['neo4j', 'falkordb'])('traverses the server root returned for an empty %s path', async engineType => {
    const root = { role: 'branch', path: { version: 'catalog.path/v1', engine_id: 8, segments: [{ term: 'server', kind: 'server', name: '' }] } }
    const database = { role: 'branch', path: { ...root.path, segments: [...root.path.segments, { term: 'database', kind: 'namespace', name: 'business' }] } }
    const graph = { role: 'leaf', kind: 'graph', name: '中文.a/b', path: { ...root.path, segments: [...(engineType === 'neo4j' ? database : root).path.segments, { term: 'graph', kind: 'graph', name: '中文.a/b' }] } }
    const levels = engineType === 'neo4j' ? [root, database, graph] : [root, graph]
    client.post.mockImplementation((_, { path }) => {
      const next = levels.find(node => node.path.segments.length === path.segments.length + 1)
      return Promise.resolve({ nodes: next ? [next] : [] })
    })
    expect(await engineAPI.getGraphTargets(8)).toEqual([graph])
    expect(client.post.mock.calls.map(([, body]) => body.path)).toEqual([{ segments: [] }, ...levels.slice(0, -1).map(node => node.path)])
  })
  it('passes the explicit graph target to schema inference in the request body', async () => {
    const target = { engine_id: 8, catalog_path: { segments: [] } }
    await ontologyAPI.inferSchemaFromEngine(target, 4)
    expect(client.post).toHaveBeenCalledWith('/graph/ontologies/infer-schema/from-engine', { ...target, ontology_id: 4 })
  })
})
