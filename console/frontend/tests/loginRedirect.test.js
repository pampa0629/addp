import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Console authenticated redirect', () => {
  it('uses the shared fail-closed redirect resolver and replaces the login history entry', () => {
    const source = readFileSync(new URL('../src/views/Login.vue', import.meta.url), 'utf8')

    expect(source).toContain("import { AuthLoginFlow, resolveLoginRedirect } from '@common-ui'")
    expect(source).toContain('router.replace(resolveLoginRedirect(route.query.redirect))')
    expect(source).not.toContain('route.query.redirect.startsWith')
    expect(source).not.toContain('router.push(redirect)')
  })
})
