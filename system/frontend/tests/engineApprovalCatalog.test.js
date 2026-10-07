import { describe, expect, it, vi } from 'vitest'
import { approvalTargetFromSelection, approvalTargetsEqual, createApprovalCatalogAdapter, isApprovalTable, safePickerEngineID } from '../src/utils/engineApprovalCatalog'

const rootPath = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }] }
const root = { name: '', path: rootPath, role: 'branch', term: 'server', kind: 'server' }
const namespace = { name: '户外', path: { ...rootPath, segments: [...rootPath.segments, { term: 'schema', kind: 'namespace', name: '户外' }] }, role: 'branch', term: 'schema', kind: 'namespace' }
const table = { name: 'a/b', path: { ...rootPath, segments: [...namespace.path.segments, { term: 'table', kind: 'table', name: 'a/b' }] }, role: 'leaf', term: 'table', kind: 'table' }
const engine = { id: '2', name: 'Database', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }
describe('System live-catalog protocol adapter', () => {
  it('compares target facts independently of JSON object key ordering', () => {
    const reordered = { ...table.path, engine_id: '2', segments: table.path.segments.map(segment => ({ kind: segment.kind, name: segment.name, term: segment.term })) }
    expect(approvalTargetsEqual(reordered, table.path)).toBe(true)
    expect(approvalTargetsEqual({ ...reordered, engine_id: '3' }, table.path)).toBe(false)
    expect(approvalTargetsEqual({ ...reordered, segments: [...reordered.segments].reverse() }, table.path)).toBe(false)
    expect(approvalTargetsEqual({ ...reordered, segments: [null] }, table.path)).toBe(false)
  })
  it('browses root, namespace and leaf through System only, preserving the provider path', async () => {
    const api = vi.fn().mockResolvedValueOnce({ nodes: [root] }).mockResolvedValueOnce({ nodes: [namespace] }).mockResolvedValueOnce({ nodes: [table] })
    const adapter = createApprovalCatalogAdapter(engine, api)
    const node = await adapter.getTreeRoot('2')
    const [leaf] = await adapter.getNodeChildren(node.children[0])
    expect(api.mock.calls).toEqual([['2', { segments: [] }], ['2', rootPath], ['2', namespace.path]])
    expect(leaf.locator).toBe('addp://engine/2/path/%E6%88%B7%E5%A4%96/a%2Fb?type=table')
    const target = approvalTargetFromSelection({ raw: { node: leaf } }, '2')
    expect(target).toEqual({ ...table.path, engine_id: '2' })
    target.segments[1].name = 'changed'
    expect(table.path.segments[1].name).toBe('户外')
    expect(isApprovalTable(node)).toBe(false)
    expect(isApprovalTable(leaf)).toBe(true)
    expect(isApprovalTable({ metadata: { catalog_entry: { ...table, kind: 'view' } } })).toBe(false)
    expect(isApprovalTable({ metadata: { catalog_entry: { ...table, path: { ...table.path, version: 'v1' } } } })).toBe(false)
    expect(isApprovalTable({ metadata: { catalog_entry: { ...table, path: { ...table.path, segments: namespace.path.segments } } } })).toBe(false)
  })
  it('rejects incomplete, cross-engine, rounded or non-leaf facts instead of fabricating candidates', async () => {
    for (const id of ['9007199254740993', '01', '1x', 0]) expect(() => safePickerEngineID(id)).toThrow()
    const api = vi.fn().mockResolvedValue({ nodes: [{ ...root, path: { ...rootPath, engine_id: 3 } }] })
    await expect(createApprovalCatalogAdapter(engine, api).getTreeRoot('2')).rejects.toThrow()
    api.mockResolvedValue({ data: [] })
    await expect(createApprovalCatalogAdapter(engine, api).getTreeRoot('2')).rejects.toThrow()
    expect(() => approvalTargetFromSelection({ raw: { node: { metadata: { catalog_entry: namespace } } } }, '2')).toThrow()
  })
})
