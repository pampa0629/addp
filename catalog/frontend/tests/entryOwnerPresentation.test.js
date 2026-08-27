import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { isProfessionalOwner, professionalOwnerName } from '../src/utils/entryOwnerPresentation.js'

describe('Catalog entry professional owner presentation', () => {
  it('recognizes every professional source owner including Workbench', () => {
    for (const [sourceModule, ownerName] of Object.entries({
      model: 'Model', standard: 'Standard', service: 'Service', develop: 'Develop', workbench: 'Workbench'
    })) {
      expect(isProfessionalOwner(sourceModule)).toBe(true)
      expect(professionalOwnerName(sourceModule)).toBe(ownerName)
    }
  })

  it('rejects sources that do not expose a professional resolver', () => {
    expect(isProfessionalOwner('meta')).toBe(false)
    expect(professionalOwnerName('meta')).toBe('')
  })

  it('opens owner details at the top-level application origin', () => {
    const source = readFileSync(new URL('../src/views/EntryDetail.vue', import.meta.url), 'utf8')
    expect(source).toContain(':href="ownerDetailUrl"')
    expect(source).toContain('target="_top"')
    expect(source).not.toContain('@click="openOwnerDetail"')
  })
})
