import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const repoRoot = fileURLToPath(new URL('../../../', import.meta.url))
const sourceExtensions = new Set(['.js', '.jsx', '.ts', '.tsx', '.vue'])
const machineOnlyRouteFragments = [
  '/task-provider/',
  '/catalog-resources/changes',
  '/runtime/catalog-references/resolve',
  '/runtime/catalog-summaries',
  '/runtime/references/resolve',
  '/runtime/data-items/',
  '/runtime/protection-projection',
  '/runtime/content-documents',
  '/runtime/resource-grants',
  '/materialization-read-contexts',
  '/data-items/changes',
  '/lineage/executions/',
  '/lineage/services',
  '/references/resolve',
  '/references/candidates'
]

function frontendSourceFiles(directory) {
  const files = []
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...frontendSourceFiles(path))
      continue
    }
    const extension = entry.name.slice(entry.name.lastIndexOf('.'))
    if (sourceExtensions.has(extension)) files.push(path)
  }
  return files
}

describe('machine-only API isolation', () => {
  it('keeps browser source code away from service-client-only routes', () => {
    const violations = []
    for (const moduleEntry of readdirSync(repoRoot, { withFileTypes: true })) {
      if (!moduleEntry.isDirectory()) continue
      const sourceRoot = join(repoRoot, moduleEntry.name, 'frontend', 'src')
      if (!existsSync(sourceRoot) || !statSync(sourceRoot).isDirectory()) continue
      for (const file of frontendSourceFiles(sourceRoot)) {
        const source = readFileSync(file, 'utf8')
        for (const fragment of machineOnlyRouteFragments) {
          if (source.includes(fragment)) {
            violations.push(`${file.slice(repoRoot.length)} -> ${fragment}`)
          }
        }
      }
    }
    expect(violations).toEqual([])
  })
})
