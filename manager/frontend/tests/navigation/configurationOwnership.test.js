import { readdirSync, readFileSync } from 'node:fs'
import { expect, test } from 'vitest'

test('Manager module frontend leaves quick-view policy management to Console', () => {
  const root = new URL('../../src/', import.meta.url)
  const files = readdirSync(root, { recursive: true }).filter(file => /\.(js|vue)$/.test(file))
  const policyCallers = files.filter(file =>
    readFileSync(new URL(file, root), 'utf8').includes('/manager/settings/quick-view-policy'))
  expect(policyCallers).toEqual([])
})
