import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const source = readFileSync(
  fileURLToPath(new URL('../src/views/ExecutionDetail.vue', import.meta.url)),
  'utf8'
)

assert.match(source, /Object\.entries\(execution\.value\?\.outputs \|\| \{\}\)/)
assert.doesNotMatch(source, /result\.outputs/)

console.log('execution detail output contract tests passed')
