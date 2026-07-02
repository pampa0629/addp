import { describe, expect, it } from 'vitest'
import { quickViewReasonText } from '../../src/utils/quickViewReasonText.js'

const t = (key) => `translated:${key}`

describe('quickViewReasonText', () => {
  it('maps backend quick view reason codes to i18n keys', () => {
    expect(quickViewReasonText(t, 'tile generation requires numeric SRID'))
      .toBe('translated:manager.spatialPreview.tileGenerationRequiresNumericSRID')
    expect(quickViewReasonText(t, 'direct GeoJSON quick view exceeds row limit'))
      .toBe('translated:manager.spatialPreview.directGeoJSONRowLimitExceeded')
    expect(quickViewReasonText(t, 'requires_ksplat_generation'))
      .toBe('translated:manager.spatialPreview.requiresKSplatGeneration')
  })

  it('keeps unknown reasons visible for diagnostics', () => {
    expect(quickViewReasonText(t, 'custom diagnostic')).toBe('custom diagnostic')
  })
})
