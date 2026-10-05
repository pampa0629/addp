import { describe, expect, it, vi } from 'vitest'
import { shallowRef } from 'vue'
import { useDAGViewport } from '../../../common-frontend/dag/src/composables/useDAGViewport.js'

describe('shared DAG layout completion used by lineage', () => {
  it('waits for the layout event before invoking the supplied field viewport adapter', async () => {
    let complete
    const graph = shallowRef({
      getNodes: () => [{}],
      once: vi.fn((event, callback) => { expect(event).toBe('afterlayout'); complete = callback }),
      updateLayout: vi.fn()
    })
    const fit = vi.fn()
    const { autoLayout } = useDAGViewport(graph)
    const layout = { type: 'dagre', rankdir: 'LR', controlPoints: true, ranksep: 48 }
    let resolved = false
    const pending = autoLayout({ layout, fit }).then(() => { resolved = true })
    expect(graph.value.updateLayout).toHaveBeenCalledWith(layout)
    expect(fit).not.toHaveBeenCalled()
    expect(resolved).toBe(false)
    complete()
    await pending
    expect(fit).toHaveBeenCalledTimes(1)
    expect(resolved).toBe(true)
  })

  it('keeps the default Develop layout and viewport adapter when no overrides are supplied', async () => {
    let complete
    const instance = {
      getNodes: () => [{}], once: (_event, callback) => { complete = callback }, updateLayout: vi.fn(),
      get: key => key === 'group' ? { getMatrix: () => null, resetMatrix() {}, getCanvasBBox: () => ({ x: 0, y: 0, width: 400, height: 200 }) }
        : key === 'width' ? 800 : 600,
      translate: vi.fn(), zoomTo: vi.fn(), getZoom: () => 1.5
    }
    const { autoLayout } = useDAGViewport(shallowRef(instance))
    const pending = autoLayout()
    expect(instance.updateLayout).toHaveBeenCalledWith({ type: 'dagre', rankdir: 'LR', nodesep: 48, ranksep: 96 })
    expect(instance.zoomTo).not.toHaveBeenCalled()
    complete()
    await pending
    expect(instance.zoomTo).toHaveBeenCalledWith(1.5, { x: 400, y: 300 })
  })
})
