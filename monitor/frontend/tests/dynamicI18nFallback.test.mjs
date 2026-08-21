import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const sources = [
  '../src/views/Dashboard.vue',
  '../src/views/ExecutionList.vue',
  '../src/views/AlertList.vue',
  '../src/components/ExecutionTable.vue'
].map(path => ({
  path,
  source: readFileSync(new URL(path, import.meta.url), 'utf8')
}))

test('dynamic translation fallbacks check key existence without emitting missing-key warnings', () => {
  for (const { path, source } of sources) {
    assert.match(source, /const \{ t, te \} = useI18n\(\)/, `${path} must expose te()`)
    assert.doesNotMatch(source, /translated === key/, `${path} must not detect missing keys after t()`)
  }
})
