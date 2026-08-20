import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import test from 'node:test'

const listFiles = directory => readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
  const path = join(directory, entry.name)
  return entry.isDirectory() ? listFiles(path) : [path]
})

const sourceFiles = listFiles('src')

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
