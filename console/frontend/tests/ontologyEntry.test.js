import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { matchesNavigationAccess } from '../src/utils/navigationAccess'

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
describe('Ontology Console entry', () => {
  it('registers one independent governance entry with a tenant read boundary', () => {
    const config = read('../src/config/portalConfig.js')
    expect(config).toContain("modules: ['standard', 'modeling', 'quality', 'ontology']")
    expect(config).toContain("ontology: '/ontology/ontologies'")
    expect(config).toContain("_url('ontology', 5192, 'ontology', '/ontology')")
    const menu = config.match(/ontology: \{\s+label: 'console.menus.ontology.label'[\s\S]*?\n  \},/)[0]
    expect(menu).toContain("contexts: ['tenant']")
    expect(menu).toContain("permissions: ['ontology.revision.read']")
    const boundary = {contexts:['tenant'],permissions:['ontology.revision.read']}
    expect(matchesNavigationAccess(boundary,'tenant',['ontology.revision.read'])).toBe(true)
    expect(matchesNavigationAccess(boundary,'platform',['ontology.revision.read'])).toBe(false)
    expect(matchesNavigationAccess(boundary,'tenant',[])).toBe(false)
    expect(read('../src/views/Portal.vue')).toContain('ALL_HOME_CARDS.filter(card => matchesNavigationAccess')
    expect(read('../src/config/searchIndex.js')).toContain("route: '/ontology/ontologies', contexts: ['tenant'], permissions: ['ontology.revision.read']")
  })
  it('has bilingual menu, card and API labels', () => {
    for (const language of ['zh-cn','en']) {
      const {console:messages} = JSON.parse(read(`../src/i18n/${language}.json`))
      expect(messages.modules.ontology.label).toBeTruthy()
      expect(messages.menus.ontology.list).toBeTruthy()
      expect(messages.apiDocs.modules.ontology).toBeTruthy()
    }
  })
})
