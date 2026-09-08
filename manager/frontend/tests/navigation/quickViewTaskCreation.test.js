import { describe, expect, it } from 'vitest'
import {
  generationOptionsForCapability,
  isPPTXGenerationSource,
  quickViewCreationEmptyReason,
  quickViewTaskTypeForAction
} from '../../src/utils/quickViewTaskCreation.js'

describe('quick-view task creation', () => {
  it('keeps only backend-declared generation actions for the requested task type', () => {
    const options = generationOptionsForCapability({
      available_actions: ['switch_quick_view', 'generate_model3d_3d_tiles', 'generate_model3d_s3m']
    }, 'model3d_tiles_generation')

    expect(options.map(option => option.action)).toEqual([
      'generate_model3d_3d_tiles',
      'generate_model3d_s3m'
    ])
  })

  it('does not expose an action that the backend capability omitted', () => {
    expect(generationOptionsForCapability({ available_actions: [] }, 'raster_cog_generation')).toEqual([])
  })

  it('maps actions back to their canonical task type', () => {
    expect(quickViewTaskTypeForAction('generate_point_cloud_copc')).toBe('point_cloud_copc_generation')
  })

  it('recognizes a PPTX item from resource facts or locator path', () => {
    expect(isPPTXGenerationSource({ resource: { format: 'pptx' } })).toBe(true)
    expect(isPPTXGenerationSource({ identity: { locator: 'addp://engine/4/path/docs/slides.pptx?type=object&item_id=9' } })).toBe(true)
    expect(isPPTXGenerationSource({ resource: { format: 'pdf' } })).toBe(false)
  })

  it('distinguishes an existing GLB result from an unsupported source', () => {
    expect(quickViewCreationEmptyReason({
      model_3d: { result_id: 42 }
    }, 'model_3d_glb_generation')).toBe('currentResult')

    expect(quickViewCreationEmptyReason({
      render_source: 'point_cloud_copc'
    }, 'point_cloud_copc_generation')).toBe('unsupported')
  })

  it('recognizes current raster and model tile results', () => {
    expect(quickViewCreationEmptyReason({
      render_source: 'client_cog_render'
    }, 'raster_cog_generation')).toBe('currentResult')

    expect(quickViewCreationEmptyReason({
      model3d_tiles: { formats: [{ format: '3d_tiles', status: 'ready', result_id: 7 }] }
    }, 'model3d_tiles_generation')).toBe('currentResult')
  })

  it('keeps a capability without a current result classified as unsupported', () => {
    expect(quickViewCreationEmptyReason({
      available_actions: []
    }, 'gaussian_splat_ksplat_generation')).toBe('unsupported')
  })
})
