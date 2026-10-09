import { describe, expect, it } from 'vitest'
import { formatLocatorDisplayPath } from '@addp/common-frontend'
import { retrievalResultPath, retrievalFieldText, retrievalSnippet } from '../src/utils/dataRetrievalPresentation.js'

describe('data retrieval presentation', () => {
  it('preserves only bare highlight tags while displaying source HTML as text', () => {
    const value = '<img src=x onerror="alert(1)"><mark>aamember</mark><mark onclick="alert(2)">value</mark>'
    const html = retrievalFieldText({ name: 'aamember', highlights: { name: value } }, 'name')
    expect(html).toContain('&lt;img src=x onerror=&quot;alert(1)&quot;&gt;')
    expect(html).toContain('<mark>aamember</mark>')
    expect(html).toContain('&lt;mark onclick=&quot;alert(2)&quot;&gt;')
    expect(html).not.toContain('<img')
    expect(html).not.toContain('<mark onclick')
    expect(retrievalFieldText({ name: '<mark>literal</mark>' }, 'name')).toBe('&lt;mark&gt;literal&lt;/mark&gt;')
    expect(retrievalFieldText({ name: 'leader.nickname', comment: 'Team member', highlights: { comment: 'Team <mark>member</mark>' } }, 'name')).toBe('leader.nickname')
  })

  it('shows actual name and description highlights without guessing from a query', () => {
    expect(retrievalSnippet({ highlights: { name: ['<mark>Outdoors</mark>'] } })).toBe('<mark>Outdoors</mark>')
    expect(retrievalSnippet({ highlights: { description: ['A <mark>member</mark> table'] } })).toContain('<mark>member</mark>')
    expect(retrievalSnippet({ content_preview: '<script>alert(1)</script>' })).toBe('&lt;script&gt;alert(1)&lt;/script&gt;')
    expect(retrievalSnippet({ match_methods: ['vector'] })).toBe('')
  })
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
