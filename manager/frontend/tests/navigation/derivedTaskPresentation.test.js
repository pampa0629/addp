import { describe, expect, it } from 'vitest'
import {
  derivedTaskLocatorLabel,
  derivedTaskResultName,
  derivedTaskSource,
  derivedTaskSourceEngineID,
  derivedTaskSourceLocator,
  derivedTaskTargetEngineID
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
})
