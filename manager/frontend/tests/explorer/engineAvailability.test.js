import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useExplorerStore } from '../../src/stores/explorer'

const { clientGet } = vi.hoisted(() => ({ clientGet: vi.fn() }))

vi.mock('@/api/client', () => ({ default: { get: clientGet } }))
vi.mock('@/api/dataExplorer', () => ({ dataExplorerAPI: {} }))

describe('Data Explorer engine availability', () => {
  beforeEach(() => {
	setActivePinia(createPinia())
	clientGet.mockReset()
  })

  it('keeps offline engines in the catalog tree but marks them unavailable', () => {
    const store = useExplorerStore()
    store.engines = [
      {
        id: 21,
        name: 'SuperMap SDX+ for PostgreSQL',
        engine_type: 'postgresql',
        lifecycle_state: 'active',
        connection_status: 'offline'
      }
    ]

    expect(store.catalogRootNodes).toHaveLength(1)
    expect(store.catalogRootNodes[0]).toMatchObject({
      engineId: 21,
      engineAvailable: false,
      engineState: 'offline'
    })
    expect(store.isEngineAvailable(21)).toBe(false)
  })

  it('clears live preview state without losing the selected historical resource', () => {
    const store = useExplorerStore()
    store.selectedLocator = 'addp://engine/21/path/sdx/roads?type=table&item_id=1'
    store.selectedNodeContext = { locator: store.selectedLocator, label: 'roads' }
    store.previewData = { rows: [{ id: 1 }] }
    store.activeChildPreviewData = { rows: [{ id: 2 }] }

    store.clearPreview()

    expect(store.selectedLocator).toContain('engine/21')
    expect(store.selectedNodeContext.label).toBe('roads')
    expect(store.previewData).toBeNull()
	expect(store.activeChildPreviewData).toBeNull()
  })

  it('coalesces concurrent engine discovery into one request', async () => {
	const store = useExplorerStore()
	clientGet.mockResolvedValue({ data: [{ id: 2, name: 'Business PostgreSQL' }] })

	const first = store.loadEngines()
	const second = store.loadEngines()

	await Promise.all([first, second])
	expect(clientGet).toHaveBeenCalledTimes(1)
	expect(store.engines).toHaveLength(1)
  })
})
