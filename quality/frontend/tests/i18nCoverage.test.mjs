import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import test from 'node:test'

const sourceFiles = execFileSync('rg', ['--files', 'src'], { encoding: 'utf8' })
  .trim()
  .split('\n')
  .filter(Boolean)

const usedKeys = new Set()
for (const file of sourceFiles) {
  const source = readFileSync(file, 'utf8')
  for (const match of source.matchAll(/\bt\(['"]([^'"]+)['"]/g)) {
    usedKeys.add(match[1])
  }
}

const flattenKeys = (value, prefix = '', result = new Set()) => {
  for (const [key, child] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (child && typeof child === 'object' && !Array.isArray(child)) {
      flattenKeys(child, path, result)
    } else {
      result.add(path)
    }
  }
  return result
}

for (const locale of ['zh-cn', 'en']) {
  test(`${locale} defines every statically referenced translation key`, () => {
    const messages = JSON.parse(readFileSync(`src/i18n/${locale}.json`, 'utf8'))
    const availableKeys = flattenKeys(messages)
    const missingKeys = [...usedKeys].filter(key => !availableKeys.has(key)).sort()
    assert.deepEqual(missingKeys, [])
  })
}
