import { createHash } from 'node:crypto'
import { describe, expect, it, vi } from 'vitest'
import { approvalTargetFromSelection, approvalTargetsEqual, collectApprovalTargets, createApprovalCatalogAdapter, isApprovalSelection } from '../src/utils/engineApprovalCatalog'
const engine = { id: '2', name: 'NFS', engine_type: 'nfs' }
const fingerprint = name => createHash('sha256').update(`2:${name}`).digest('hex')
const ancestors = [{ id: '1', name: 'NFS', node_type: 'root' }, { id: '2', name: 'exports', node_type: 'dir' }]
const item = { engine_id: 2, item_fingerprint: fingerprint('exports/test.gdb'), name: 'test.gdb', full_name: 'exports/test.gdb', item_type: 'file', ancestors: [...ancestors, { id: '3', name: 'test.gdb', node_type: 'dir' }] }
const path = { engine_id: 2, version: 'catalog.path/v1', segments: [{ term: 'root', kind: 'root', name: '' }, { term: 'directory', kind: 'directory', name: 'exports' }, { term: 'directory', kind: 'directory', name: 'test.gdb' }] }
const facts = { schema_version: 'meta.data_item_authorization_facts/v1', engine_id: 2, item_fingerprint: item.item_fingerprint, full_name: item.full_name, item_type: 'file', basis_state: 'complete', boundary_hash: 'a'.repeat(64), boundary: { layout: 'whole', scope: { format: 'filegdb', rule_version: 1, root: path } } }
const selection = node => ({ raw: { node } })
const page = data => ({ data, total: data.length, page: 1, page_size: 100, total_pages: data.length ? 1 : 0 })
function fixture(rows = [item]) {
  const list = vi.fn(async () => page(rows)), read = vi.fn(async () => structuredClone(facts))
  return { list, read, adapter: createApprovalCatalogAdapter(engine, list, read) }
}
describe('Meta DataItem authorization picker', () => {
  it('renders whole FileGDB once and resolves its owner boundary, with no native directory or fragments', async () => {
    const { adapter, list, read } = fixture()
    const root = await adapter.getTreeRoot(), directory = root.children[0], leaf = directory.children[0]
    expect(directory.label).toBe('exports')
    expect(leaf.label).toBe('test.gdb')
    expect(leaf.hasChildren).toBe(false)
    expect(leaf.children).toEqual([])
    expect(leaf.locator).toBe('addp://engine/2/path/exports/test.gdb?type=file')
    expect(read).not.toHaveBeenCalled()
    expect(await approvalTargetFromSelection(selection(structuredClone(leaf)), '2', adapter)).toEqual({ ...path, engine_id: '2' })
    expect(read).toHaveBeenCalledWith('2', item.item_fingerprint)
    expect(list.mock.calls).toEqual([['2', { page: 1, page_size: 100 }]])
    expect(isApprovalSelection(root)).toBe(false)
  })
  it('expands current DataItems only, rereads exact facts and fails closed on incomplete facts', async () => {
    const { adapter, read } = fixture()
    const root = await adapter.getTreeRoot()
    expect(await collectApprovalTargets(selection(root.children[0]), '2', adapter)).toEqual([{ ...path, engine_id: '2' }])
    await expect(collectApprovalTargets(selection(root.children[0]), '2', adapter, 0)).rejects.toThrow('tooManyTargets')
    read.mockResolvedValue({ ...facts, basis_state: 'needs_refresh', boundary: null, boundary_hash: null })
    await expect(collectApprovalTargets(selection(root.children[0]), '2', adapter)).rejects.toThrow('invalidDataItemBoundary')
    await expect(collectApprovalTargets(selection(root), '2', adapter)).rejects.toThrow()
  })
  it('rejects cross-engine, malformed ancestry and incomplete pages instead of exposing a partial tree', async () => {
    for (const rows of [[{ ...item, engine_id: 3 }], [{ ...item, ancestors: [...ancestors, ancestors[0]] }]]) {
      await expect(fixture(rows).adapter.getTreeRoot()).rejects.toThrow()
    }
    const { adapter, list } = fixture()
    list.mockResolvedValue({ ...page([item]), total: 101 })
    await expect(adapter.getTreeRoot()).rejects.toThrow('incompletePermissionTree')
    list.mockResolvedValue({ ...page(Array(100).fill(item)), total: 20001 })
    await expect(adapter.getTreeRoot()).rejects.toThrow('permissionTreeCapacity')
  })
  it('refreshes identities and keeps searches read-only without fetching authorization facts', async () => {
    const { adapter, read, list } = fixture()
    const found = await adapter.search('2', 'test.gdb')
    expect(found.map(node => node.label)).toEqual(['test.gdb'])
    expect(read).not.toHaveBeenCalled()
    await expect(adapter.search('3', 'test')).rejects.toThrow()
    await expect(adapter.search('2', 'test', { limit: 0 })).rejects.toThrow()
    adapter.invalidate(); list.mockResolvedValue(page([]))
    expect(await adapter.search('2', 'test')).toEqual([])
    await expect(adapter.resolveTarget(found[0])).rejects.toThrow()
  })
  it('never installs a stale identity snapshot after refresh', async () => {
    const { adapter, list } = fixture()
    let release
    list.mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
    const old = adapter.getTreeRoot()
    const rejection = expect(old).rejects.toThrow('stalePermissionTree')
    adapter.invalidate()
    list.mockResolvedValue(page([]))
    expect((await adapter.getTreeRoot()).children).toEqual([])
    release(page([item])); await rejection
    expect(await adapter.search('2','test')).toEqual([])
    expect(list).toHaveBeenCalledTimes(2)
  })
  it('preserves schema and table names with separators, using Meta facts for the typed path', async () => {
    const tablePath = { ...path, segments: [{ term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'a.b' }, { term: 'table', kind: 'table', name: 'x/y' }] }
    const row = { ...item, item_type: 'table', name: 'x/y', full_name: 'a.b.x/y', item_fingerprint: fingerprint('a.b.x/y'), ancestors: [{ id: '1', name: 'PG', node_type: 'database' }, { id: '2', name: 'a.b', node_type: 'schema' }] }
    const { adapter, read } = fixture([row])
    read.mockResolvedValue({ ...facts, item_fingerprint: row.item_fingerprint, item_type: 'table', full_name: row.full_name, boundary: { layout: 'single', native_target: tablePath } })
    const leaf = (await adapter.getTreeRoot()).children[0].children[0]
    expect(leaf.locator).toBe('addp://engine/2/path/a.b/x%2Fy?type=table')
    expect(await adapter.resolveTarget(leaf)).toEqual({ ...tablePath, engine_id: '2' })
    expect(approvalTargetsEqual({ ...tablePath, engine_id: '2' }, tablePath)).toBe(true)
    expect(approvalTargetsEqual({ ...tablePath, engine_id: '3' }, tablePath)).toBe(false)
  })
})
