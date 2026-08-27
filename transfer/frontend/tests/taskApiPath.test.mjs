import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const taskApiSource = readFileSync(new URL('../src/api/tasks.js', import.meta.url), 'utf8')

test('Transfer task management lists canonical task definitions', () => {
  assert.match(taskApiSource, /client\.get\('\/transfer\/task-definitions', \{ params \}\)/)
  assert.doesNotMatch(taskApiSource, /client\.get\('\/transfer\/tasks'/)
})
