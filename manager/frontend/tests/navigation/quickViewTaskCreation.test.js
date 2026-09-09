import { describe, expect, it } from 'vitest'
import {
  currentQuickViewResultTaskType,
  generationOptionsForCapability,
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
    expect(quickViewTaskTypeForAction('generate_pptx_pdf')).toBe('pptx_pdf_generation')
  })

  it('exposes PPTX generation only when declared by the backend capability', () => {
    expect(generationOptionsForCapability({
      available_actions: ['generate_pptx_pdf']
    }, 'pptx_pdf_generation').map(option => option.action)).toEqual(['generate_pptx_pdf'])

    expect(generationOptionsForCapability({
      available_actions: []
    }, 'pptx_pdf_generation')).toEqual([])
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

  it('recognizes a ready PPTX PDF result from backend capability state', () => {
    const capability = {
      pptx_pdf: { status: 'ready', result_id: 19 }
    }
    expect(quickViewCreationEmptyReason(capability, 'pptx_pdf_generation')).toBe('currentResult')
    expect(currentQuickViewResultTaskType(capability)).toBe('pptx_pdf_generation')
  })

  it('keeps a capability without a current result classified as unsupported', () => {
    expect(quickViewCreationEmptyReason({
      available_actions: []
    }, 'gaussian_splat_ksplat_generation')).toBe('unsupported')
  })

  it('recognizes any current result when the task list is not filtered by type', () => {
    expect(quickViewCreationEmptyReason({
      default_vector_tile_cache_id: 12
    })).toBe('currentResult')
  })
})
