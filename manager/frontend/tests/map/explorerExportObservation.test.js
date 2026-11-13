import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'

const source = readFileSync(new URL('../../src/components/explorer/PreviewPanel.vue', import.meta.url), 'utf8')
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
function harness(api = {}, overrides = {}) {
  const watchers = [], unmounts = [], calls = [], notices = []
  const ref = value => ({ value })
  const state = {
    props: { selectedNode: { locator: 'first' } }, store: { selectedChildName: '', selectedChildKey: '', selectedRefPath: '', selectedNestedChildPath: '' },
    exporting: ref(false), exportDialogVisible: ref(true), defaultExportFileName: ref('protected.csv'), resourceActions: ref(null),
    exportAbortController: null, sourceDownloadController: null, downloading: ref(false),
    exportRequests: createLatestRequestScope(), resourceActionRequests: createLatestRequestScope(),
    dataExplorerAPI: { createExport: async () => ({ id: 1 }), getExport: async () => ({}), getResourceActions: async () => ({ actions: {} }), ...api },
    waitForExportSession: async () => { calls.push('poll'); return { id: 1 } },
    downloadAuthenticatedExport: async () => { calls.push('download') }, client: {}, AbortController,
    ElMessage: { success: x => notices.push(x), error: x => notices.push(x) }, t: (key, values) => values?.error || key,
    console: { error() {}, warn() {} }, loadSelectedItemEmbeddingState() {}, selectedEmbeddingState: ref(null),
    quickViewStateSaveTimer: 0, quickViewRequestSeq: 0,
    watch: (get, callback) => watchers.push({ get, callback, last: JSON.stringify(get()) }),
    onUnmounted: fn => unmounts.push(fn), ...overrides
  }
  const methods = source.slice(source.indexOf('const openExportDialog ='), source.indexOf('const title = computed'))
  const actions = vm.runInContext(`${methods}\n;({ handleExport })`, vm.createContext(state))
  const flush = () => Promise.all(watchers.flatMap(w => {
    const next = JSON.stringify(w.get()); if (next === w.last) return []
    w.last = next; return [w.callback(w.get())]
  }))
  return { state, calls, notices, ...actions, flush, unmount: () => unmounts.forEach(fn => fn()) }
}

describe('Manager export observation ownership', () => {
  it.each(['resource', 'selectedChildName', 'selectedChildKey', 'selectedRefPath', 'selectedNestedChildPath'])('ignores a late creation receipt after a %s switch', async kind => {
    const old = deferred(), h = harness({ createExport: () => old.promise })
    const work = h.handleExport({ format: 'csv', fileName: 'old.csv' })
    if (kind === 'resource') h.state.props.selectedNode = { locator: 'second' }
    else h.state.store[kind] = 'other-child'
    await h.flush()
    expect(h.state.exporting.value).toBe(false)
    expect(h.state.exportDialogVisible.value).toBe(false)
    old.resolve({ id: 1 }); await work
    expect(h.calls).toEqual([]); expect(h.notices).toEqual([])
  })
  it('does not show an obsolete creation failure after a resource switch', async () => {
    const old = deferred(), h = harness({ createExport: () => old.promise })
    const work = h.handleExport({ format: 'csv' })
    h.state.props.selectedNode = { locator: 'second' }; await h.flush()
    old.reject(new Error('obsolete protection refusal')); await work
    expect(h.notices).toEqual([])
  })
  it('does not clear the loading state of a replacement export in old finally', async () => {
    const pending = [deferred(), deferred()]; let index = 0
    const h = harness({ createExport: () => pending[index++].promise })
    const old = h.handleExport({ format: 'csv' })
    h.state.props.selectedNode = { locator: 'second' }; await h.flush()
    const current = h.handleExport({ format: 'csv' })
    pending[0].resolve({ id: 1 }); await old
    expect(h.state.exporting.value).toBe(true); expect(h.calls).toEqual([])
    pending[1].resolve({ id: 2 }); await current
    expect(h.state.exporting.value).toBe(false); expect(h.calls).toEqual(['poll', 'download'])
  })
  it('ignores a creation receipt after the panel unmounts', async () => {
    const old = deferred(), h = harness({ createExport: () => old.promise })
    const work = h.handleExport({ format: 'csv' }); h.unmount()
    old.resolve({ id: 1 }); await work
    expect(h.calls).toEqual([]); expect(h.notices).toEqual([])
  })
  it.each(['success', 'failure'])('ignores a late resource-action %s after the current capabilities arrive', async outcome => {
    const old = deferred();let reads = 0
    const current = { actions: { export: { supported: false, formats: [] } } }
    const h = harness({ getResourceActions: () => ++reads === 1 ? old.promise : Promise.resolve(current) })
    h.state.props.selectedNode = { locator: 'second' }; const before = h.flush()
    h.state.props.selectedNode = { locator: 'third' }; await h.flush()
    if (outcome === 'success') old.resolve({ actions: { export: { supported: true, formats: ['csv'] } } })
    else old.reject(new Error('old capability failure'))
    await before
    expect(h.state.resourceActions.value).toEqual(current)
  })
  it('ignores a late polling result after the selected resource changes', async () => {
    const old = deferred(), entered = deferred()
    const h = harness({}, { waitForExportSession: () => { entered.resolve(); return old.promise } })
    const work = h.handleExport({ format: 'csv' }); await entered.promise
    h.state.props.selectedNode = { locator: 'second' }; await h.flush()
    const notices = [...h.notices]
    old.resolve({ id: 1 }); await work
    expect(h.calls).toEqual([]); expect(h.notices).toEqual(notices)
    expect(h.state.exporting.value).toBe(false)
  })
  it('keeps a current protection refusal visible and releases loading', async () => {
    const h = harness({ createExport: async () => { throw { response: { data: { error: 'current protection refusal' } } } } })
    await h.handleExport({ format: 'csv' })
    expect(h.notices).toEqual(['current protection refusal'])
    expect(h.state.exporting.value).toBe(false)
    expect(h.calls).toEqual([])
  })

})
