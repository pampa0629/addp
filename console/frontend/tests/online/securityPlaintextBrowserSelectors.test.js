import { readFileSync } from 'node:fs'
import { expect, it } from 'vitest'

const source = readFileSync(new URL('../../e2e/online/security-plaintext-access.spec.js', import.meta.url), 'utf8')
const selectors = [...source.matchAll(/getByRole\('(button|dialog)', \{ name: \/([^\n]+?)\/ \}\)/g)]
  .map(([, role, pattern]) => ({ role, name: new RegExp(pattern) }))
const controls = [
  ['button', 'accessRequest.approve'],
  ['button', 'accessRequest.confirmActions.approve'],
  ['button', 'accessRequest.viewAuthorization'],
  ['button', 'exemption.revoke'],
  ['dialog', 'exemption.revokeTitle'],
  ['button', 'exemption.confirmRevoke']
]
const cases = ['zh-cn', 'en'].flatMap(language => {
  const messages = JSON.parse(readFileSync(new URL(`../../../../security/frontend/src/i18n/${language}.json`, import.meta.url), 'utf8')).security
  return controls.map(([role, key]) => [language, key, role, key.split('.').reduce((value, part) => value[part], messages)])
})

it.each(cases)('Hosted plaintext workflow locates the %s %s control', (_language, _key, role, label) => {
  expect(selectors.some(selector => selector.role === role && selector.name.test(label)), label).toBe(true)
})
