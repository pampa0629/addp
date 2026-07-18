import { describe, expect, it } from 'vitest'
import {
  createDefaultTileCacheTaskForm,
  createTileCacheTaskPayload
} from '../../src/utils/tileCacheTaskForm'

describe('tileCacheTaskForm', () => {
  it('keeps locator source kind and full name for file targets', () => {
    const form = createDefaultTileCacheTaskForm()
    Object.assign(form, {
      name: 'addp/gis/a2.shp 瓦片缓存',
      config: {
        ...form.config,
        target: {
          ...form.config.target,
          source_engine_id: 9,
          source_kind: 'object',
          full_name: 'addp/gis/a2.shp',
          locator: 'addp://engine/9/path/addp/gis/a2.shp?type=object&item_id=284',
          item_id: 284,
          item_fingerprint: 'fp-a2'
        },
        options: {
          ...form.config.options,
          geometry_column: 'geometry'
        }
      }
    })

    expect(createTileCacheTaskPayload(form).config.target).toMatchObject({
      source_engine_id: 9,
      source_kind: 'object',
      full_name: 'addp/gis/a2.shp',
      locator: 'addp://engine/9/path/addp/gis/a2.shp?type=object&item_id=284',
      item_id: 284,
      item_fingerprint: 'fp-a2'
    })
  })

  it('does not expose owner schedule fields', () => {
    const form = createDefaultTileCacheTaskForm()
    expect(form).not.toHaveProperty('schedule')
    expect(createTileCacheTaskPayload(form)).not.toHaveProperty('schedule')
    expect(createTileCacheTaskPayload(form)).not.toHaveProperty('next_run_at')
  })
})
