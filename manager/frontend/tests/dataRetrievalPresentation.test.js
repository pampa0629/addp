import { describe, expect, it } from 'vitest'
import { formatLocatorDisplayPath } from '@addp/common-frontend'
import { retrievalResultPath } from '../src/utils/dataRetrievalPresentation.js'

describe('data retrieval presentation', () => {
  it('keeps the indexed result path when it is available', () => {
    expect(retrievalResultPath({ full_name: 'addp/doc/report.pdf' }, formatLocatorDisplayPath))
      .toBe('addp/doc/report.pdf')
  })

  it('derives a readable path from a pure vector hit locator', () => {
    const locator = 'addp://engine/12/path/addp/image/%E5%BC%80%E4%BC%9A.jpg?type=object&item_id=51954'

    expect(retrievalResultPath({ locator }, formatLocatorDisplayPath))
      .toBe('addp/image/开会.jpg')
  })
})
