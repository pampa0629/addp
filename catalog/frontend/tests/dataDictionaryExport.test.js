import { describe, expect, it } from 'vitest'
import { dataDictionaryExportFileName, normalizeDataDictionaryBlobError } from '../src/utils/dataDictionaryExport'

describe('dataDictionaryExport', () => {
  it('uses the server snapshot generation time in a safe file name', async () => {
    const blob = new Blob([JSON.stringify({ generated_at: '2026-08-28T12:34:56.000Z' })], { type: 'application/json' })
    await expect(dataDictionaryExportFileName(blob, 'entry/id')).resolves.toBe(
      'data-dictionary-entry-id-20260828T123456Z.json'
    )
  })

  it('restores structured JSON errors returned to a Blob download request', async () => {
    const error = {
      response: {
        data: new Blob([JSON.stringify({ error: '依赖不可用', error_code: 'catalog_data_dictionary_dependency_unavailable' })]),
        headers: { 'content-type': 'application/json; charset=utf-8' }
      }
    }
    await normalizeDataDictionaryBlobError(error)
    expect(error.response.data).toEqual({
      error: '依赖不可用',
      error_code: 'catalog_data_dictionary_dependency_unavailable'
    })
  })
})
