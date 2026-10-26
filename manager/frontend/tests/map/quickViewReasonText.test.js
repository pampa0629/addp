import { describe, expect, it } from 'vitest'
import { quickViewActionErrorText, quickViewReasonText } from '../../src/utils/quickViewReasonText.js'

const t = (key) => `translated:${key}`

describe('quickViewReasonText', () => {
  it('maps backend quick view reason codes to i18n keys', () => {
    expect(quickViewReasonText(t, 'tile generation requires numeric SRID'))
      .toBe('translated:manager.spatialPreview.tileGenerationRequiresNumericSRID')
    expect(quickViewReasonText(t, 'direct FlatGeobuf quick view exceeds row limit'))
      .toBe('translated:manager.spatialPreview.directFlatGeobufRowLimitExceeded')
    expect(quickViewReasonText(t, 'requires_ksplat_generation'))
      .toBe('translated:manager.spatialPreview.requiresKSplatGeneration')
    expect(quickViewReasonText(t, 'model_3d_glb_source_not_supported'))
      .toBe('translated:manager.spatialPreview.model3DGLBSourceNotSupported')
  })

  it('keeps unknown reasons visible for diagnostics', () => {
    expect(quickViewReasonText(t, 'custom diagnostic')).toBe('custom diagnostic')
  })
})

describe('quick view action errors', () => {
  const fallback = 'manager.explorer.generateModel3DGLBFailed'
  it.each([
    [403, 'source_authorization_required', 'quickViewSourceAuthorizationRequired'],
    [409, 'source_composition_incomplete', 'quickViewSourceCompositionIncomplete'],
    [403, undefined, 'quickViewPermissionDenied'],
    [403, 'unknown_denial', 'quickViewPermissionDenied']
  ])('explains HTTP %s %s using a stable localized reason', (status, error_code, key) => {
    expect(quickViewActionErrorText(t, { response: { status, data: { error_code, error: 'private upstream detail' } } }, fallback))
      .toBe(`translated:manager.explorer.${key}`)
  })

  it.each([
    [500, 'source_authorization_required'], [503, 'source_composition_incomplete'],
    [409, 'existing_result_action_required'], [401, 'source_authorization_required'],
    [409, { toString: () => 'source_composition_incomplete' }], [409, null]
  ])('does not misclassify other statuses or malformed error codes', (status, error_code) => {
    expect(quickViewActionErrorText(t, { response: { status, data: { error_code, error: 'private upstream detail' } } }, fallback))
      .toBe(`translated:${fallback}`)
  })
})
