import assert from 'node:assert/strict'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const repository = resolve(import.meta.dirname, '../../..')
const read = path => readFileSync(resolve(repository, path), 'utf8')
const frontends = readdirSync(repository, { withFileTypes: true })
  .filter(entry => entry.isDirectory() && existsSync(resolve(repository, entry.name, 'frontend/src/router/index.js')))
  .map(entry => entry.name)
  .filter(module => /\bname:\s*['"]Login['"]/.test(read(`${module}/frontend/src/router/index.js`)))

test('discovers registered frontend login routes', () => {
  assert.ok(frontends.length > 0)
  assert.ok(frontends.includes('console'))
  assert.ok(frontends.includes('portal'))
})

for (const module of frontends) {
  test(`${module} delegates its entire login UI and flow to common-frontend`, () => {
    const source = read(`${module}/frontend/src/views/Login.vue`)
    assert.match(source, /import\s*\{[^}]*\bAuthLoginFlow\b[^}]*\}\s*from\s*['"]@common-ui['"]/)
    assert.match(source, /<AuthLoginFlow\b/)
    for (const prop of ['title', 'login', 'verify-mfa', 'select-context']) {
      assert.match(source, new RegExp(`:${prop}="[^"]+"`))
    }
    assert.match(source, /@authenticated="[^"]+"/)
    assert.match(source, /resolveLoginRedirect\(route\.query\.redirect/)
    assert.doesNotMatch(source, /<el-(?:form|input|button|card)\b|<form\b|<input\b|next_action|ElMessage|router\.push\(/)
    assert.match(read(`${module}/frontend/src/store/auth.js`), /createAuthStore\(/)
    assert.match(read(`${module}/frontend/src/api/auth.js`), /createAuthAPI\(/)
    assert.match(read(`${module}/frontend/src/main.js`), /createAddpI18n\(/)
  })
}

test('the shared login component owns credentials, MFA and context selection', () => {
  const source = read('common-frontend/basic/src/components/AuthLoginFlow.vue')
  for (const action of ['session_issued', 'verify_mfa', 'select_context']) assert.ok(source.includes(`case '${action}':`))
  assert.match(source, /emit\('authenticated'\)/)
  assert.match(read('common-frontend/basic/src/index.js'), /export\s*\{\s*default as AuthLoginFlow\s*\}/)
})
