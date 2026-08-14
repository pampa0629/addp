import { describe, expect, it } from 'vitest'
import { getDocumentTypeTagType } from '../src/utils/documentType'

describe('document type tag mapping', () => {
  it.each([
    ['national', 'danger'],
    ['industry', 'warning'],
    ['internal', 'primary'],
    ['reference', 'info']
  ])('maps %s to a valid Element Plus tag type', (documentType, tagType) => {
    expect(getDocumentTypeTagType(documentType)).toBe(tagType)
  })

  it.each([undefined, null, '', 'legacy'])('falls back to info for %s', documentType => {
    expect(getDocumentTypeTagType(documentType)).toBe('info')
  })
})
