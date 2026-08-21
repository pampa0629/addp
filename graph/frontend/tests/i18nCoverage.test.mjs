import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = path.join(frontendRoot, 'src')

function readJson(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(frontendRoot, relativePath), 'utf8'))
}

function flattenKeys(value, prefix = '', result = new Set()) {
  for (const [key, child] of Object.entries(value)) {
    const fullKey = prefix ? `${prefix}.${key}` : key
    if (child && typeof child === 'object' && !Array.isArray(child)) {
      flattenKeys(child, fullKey, result)
    } else {
      result.add(fullKey)
    }
  }
  return result
}

function sourceFiles(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const fullPath = path.join(directory, entry.name)
    if (entry.isDirectory()) return sourceFiles(fullPath)
    return /\.(?:js|vue)$/.test(entry.name) ? [fullPath] : []
  })
}

function usedTranslationKeys() {
  const keys = new Set()
  for (const file of sourceFiles(sourceRoot)) {
    const source = fs.readFileSync(file, 'utf8')
    for (const match of source.matchAll(/(?:\$t|\bt)\(\s*['"](graph\.[^'"]+)['"]/g)) {
      keys.add(match[1])
    }
  }
  return keys
}

describe('graph frontend translations', () => {
  const zhCnKeys = flattenKeys(readJson('src/i18n/zh-cn.json'))
  const enKeys = flattenKeys(readJson('src/i18n/en.json'))

  it('keeps Chinese and English resource keys aligned', () => {
    expect([...zhCnKeys].filter(key => !enKeys.has(key))).toEqual([])
    expect([...enKeys].filter(key => !zhCnKeys.has(key))).toEqual([])
  })

  it('defines every statically referenced graph translation key', () => {
    const usedKeys = [...usedTranslationKeys()]
    expect(usedKeys.filter(key => !zhCnKeys.has(key))).toEqual([])
    expect(usedKeys.filter(key => !enKeys.has(key))).toEqual([])
  })
})
