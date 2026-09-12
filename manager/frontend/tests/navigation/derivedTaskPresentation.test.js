import { describe, expect, it } from 'vitest'
import {
  derivedTaskLocatorLabel,
  derivedTaskResultName,
  derivedTaskSource,
  derivedTaskSourceEngineID,
  derivedTaskSourceLocator,
  derivedTaskTargetCatalogIdentity,
  derivedTaskTargetEngineID,
  metaItemLocator
} from '../../src/utils/derivedTaskPresentation.js'

describe('derived task presentation', () => {
  it('uses target-backed source facts for legacy-shaped managed quick-view configs', () => {
    const task = {
      task_type: 'raster_cog_generation',
      config: { target: { engine_id: 12, item_locator: 'addp://engine/12/path/a.tif?type=file&item_id=3' } }
    }
    expect(derivedTaskSource(task)).toEqual(task.config.target)
    expect(derivedTaskSourceEngineID(task)).toBe(12)
    expect(derivedTaskSourceLocator(task)).toContain('a.tif')
  })

  it('keeps source and target engines distinct for spatial business tasks', () => {
    const task = { task_type: 'raster_mosaic_generation', config: { source: { source_engine_id: 2 }, target: { target_engine_id: 9 } } }
    expect(derivedTaskSourceEngineID(task)).toBe(2)
    expect(derivedTaskTargetEngineID(task)).toBe(9)
  })

  it('presents a readable source path and result name', () => {
    const parser = () => ({ path: ['bucket', 'slides.pptx'] })
    expect(derivedTaskLocatorLabel('locator', parser)).toBe('bucket / slides.pptx')
    expect(derivedTaskResultName({ config: { result: { file_name: 'slides.pdf' } } })).toBe('slides.pdf')
  })

  it('resolves spatial business result identities without exposing Manager infra results', () => {
    const parse = locator => ({ path: locator.includes('mosaics') ? ['addp', 'mosaics'] : ['addp', 'tiles'] })
    expect(derivedTaskTargetCatalogIdentity({
      task_type: 'vector_tile_set_generation',
      config: { target: { engine_id: 12, storage_locator: 'tiles', name: 'roads.pmtiles' } }
    }, parse)).toEqual({ engineId: 12, catalogPath: 'addp/tiles/roads.pmtiles' })
    expect(derivedTaskTargetCatalogIdentity({
      task_type: 'raster_mosaic_generation',
      config: { placement: { mode: 'detached' }, target: { target_engine_id: 12, storage_locator: 'mosaics', dataset_name: 'terrain' } }
    }, parse)).toEqual({ engineId: 12, catalogPath: 'addp/mosaics/terrain' })
    expect(derivedTaskTargetCatalogIdentity({
      task_type: 'raster_mosaic_generation',
      config: { placement: { mode: 'in_place' }, target: { target_engine_id: 12, storage_locator: 'mosaics', dataset_name: 'ignored' } }
    }, parse)).toEqual({ engineId: 12, catalogPath: 'addp/mosaics' })
    expect(derivedTaskTargetCatalogIdentity({ task_type: 'model_3d_glb_generation', config: {} }, parse)).toBeNull()
  })

  it('builds a canonical item locator only from a resolved Meta item', () => {
    const build = value => JSON.stringify(value)
    expect(metaItemLocator({ id: 51, engine_id: 12, item_type: 'object', full_name: 'addp/tiles/roads.pmtiles' }, build)).toBe(JSON.stringify({
      engineId: 12,
      path: ['addp', 'tiles', 'roads.pmtiles'],
      type: 'object',
      itemId: 51
    }))
    expect(metaItemLocator({ engine_id: 12, item_type: 'object', full_name: 'addp/tiles/roads.pmtiles' }, build)).toBe('')
  })
})
