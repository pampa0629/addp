import assert from 'node:assert/strict'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { relative, resolve } from 'node:path'
import test from 'node:test'

const repositoryRoot = resolve(import.meta.dirname, '../../..')
const source = relativePath => readFileSync(resolve(repositoryRoot, relativePath), 'utf8')
const vueFilesUnder = relativeDirectory => readdirSync(resolve(repositoryRoot, relativeDirectory), {
  recursive: true,
  withFileTypes: true
})
  .filter(entry => entry.isFile() && entry.name.endsWith('.vue'))
  .map(entry => resolve(entry.parentPath, entry.name))

test('ResourceTreePicker is the single form-level resource tree implementation', () => {
  const picker = source('common-frontend/basic/src/components/ResourceTreePicker.vue')

  assert.match(picker, /import ResourceTree from '\.\/ResourceTree\.vue'/)
  assert.match(picker, /<ResourceTree(?:\s|>)/)
  assert.doesNotMatch(picker, /<el-tree(?:\s|>)/)
})

test('manager resource-selection forms compose ResourceTreePicker instead of rebuilding resource trees', () => {
  const consumers = [
    'manager/frontend/src/views/TileCache.vue',
    'manager/frontend/src/views/VectorMaterializedView.vue',
    'manager/frontend/src/views/VectorizationTasks.vue'
  ]

  for (const relativePath of consumers) {
    const content = source(relativePath)
    assert.match(content, /ResourceTreePicker/)
    assert.doesNotMatch(content, /<ResourceTree(?:\s|>)/)
    assert.doesNotMatch(content, /<el-tree(?:\s|>)/)
    assert.doesNotMatch(content, /dataExplorerAPI\.get(?:Tree|NodeChildren|TreeAncestors)\(/)
  }
})

test('manager keeps direct ResourceTree ownership only in the Data Explorer browser', () => {
  const explorerTree = source('manager/frontend/src/components/explorer/ExplorerTree.vue')

  assert.match(explorerTree, /<ResourceTree(?:\s|>)/)
  assert.doesNotMatch(explorerTree, /ResourceTreePicker/)
})

test('direct ResourceTree composition has an explicit repository-wide owner list', () => {
  const frontendSourceRoots = readdirSync(repositoryRoot, { withFileTypes: true })
    .filter(entry => entry.isDirectory())
    .map(entry => resolve(repositoryRoot, entry.name, 'frontend/src'))
    .filter(existsSync)
  frontendSourceRoots.push(resolve(repositoryRoot, 'common-frontend/basic/src'))

  const directOwners = frontendSourceRoots
    .flatMap(root => vueFilesUnder(relative(repositoryRoot, root)))
    .filter(filePath => /<ResourceTree(?:\s|>)/.test(readFileSync(filePath, 'utf8')))
    .map(filePath => relative(repositoryRoot, filePath))
    .sort()

  assert.deepEqual(directOwners, [
    'common-frontend/basic/src/components/ResourceTreePicker.vue',
    'manager/frontend/src/components/explorer/ExplorerTree.vue',
    'orchestrator/frontend/src/components/TaskPanel.vue'
  ])
})

test('manager views do not introduce another form-level resource tree implementation', () => {
  for (const filePath of vueFilesUnder('manager/frontend/src/views')) {
    const content = readFileSync(filePath, 'utf8')
    assert.doesNotMatch(content, /<ResourceTree(?:\s|>)/, filePath)
    assert.doesNotMatch(content, /dataExplorerAPI\.get(?:Tree|NodeChildren|TreeAncestors)\(/, filePath)
  }
})
