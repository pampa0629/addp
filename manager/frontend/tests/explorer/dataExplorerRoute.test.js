import { describe, expect, it } from 'vitest'

import {
  buildCatalogEntryConsoleRoute,
  buildDataExplorerQuery,
  normalizeDataExplorerTab,
  resolveDataExplorerRouteState
} from '../../src/utils/dataExplorerRoute'

describe('dataExplorerRoute', () => {
  const locator = 'addp://engine/11/path/public/farmland?type=table&item_id=88'

  it('keeps only supported item tabs', () => {
    expect(normalizeDataExplorerTab('profile')).toBe('profile')
    expect(normalizeDataExplorerTab('ATTRIBUTES')).toBe('attributes')
    expect(normalizeDataExplorerTab('unknown')).toBe('preview')
  })

  it('omits the default preview tab from route query', () => {
    expect(buildDataExplorerQuery(locator, 'preview')).toEqual({
      locator,
      tab: undefined
    })
  })

  it('builds a Console-owned CatalogEntry route for cross-module navigation', () => {
    expect(buildCatalogEntryConsoleRoute(' 30b94349-9434-407d-8577-b3f1472cd7ea ')).toBe(
      '/catalog/entries/30b94349-9434-407d-8577-b3f1472cd7ea'
    )
    expect(buildCatalogEntryConsoleRoute('')).toBe('/catalog/entries')
  })

  it('does not retain a tab without a selected resource', () => {
    expect(buildDataExplorerQuery('', 'attributes')).toEqual({
      locator: undefined,
      tab: undefined
    })
  })

  it('canonicalizes the full recoverable query', () => {
    expect(resolveDataExplorerRouteState({
      locator: ` ${locator} `,
      tab: ['ATTRIBUTES', 'profile'],
      legacy: 'old'
    })).toEqual({
      locator,
      tab: 'attributes',
      query: { locator, tab: 'attributes' },
      changed: true
    })
  })

  it('removes a tab and unknown query when no resource is selected', () => {
    expect(resolveDataExplorerRouteState({ tab: 'profile', legacy: 'old' })).toEqual({
      locator: '',
      tab: 'preview',
      query: {},
      changed: true
    })
  })
})
