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

test('resource tree transport reuses the bound session and the single shared API client', () => {
  const api = source('common-frontend/basic/src/api/resourceTree.js')
  assert.match(api, /createAPIClient\(getBoundAuthStore,/)
  assert.doesNotMatch(api, /axios\.create|interceptors\.|getAccessToken|createBrowserAuthSession/)
})

test('ResourceTreePicker is the single form-level resource tree implementation', () => {
  const picker = source('common-frontend/basic/src/components/ResourceTreePicker.vue')

  assert.match(picker, /import ResourceTree from '\.\/ResourceTree\.vue'/)
  assert.match(picker, /<ResourceTree(?:\s|>)/)
  assert.doesNotMatch(picker, /<el-tree(?:\s|>)/)
})

test('multiple resource input composition retains the shared picker and standard DAG owner', () => {
  const panel = source('develop/frontend/src/components/workflow/OperatorParamsPanel.vue')
  assert.match(panel, /:multiple="Boolean\(resourcePickerInputParam\)"/)
  assert.doesNotMatch(panel, /<el-tree\b|<ResourceTree\s|dataExplorerAPI|uploadFiles/)
  const editor = source('develop/frontend/src/views/WorkflowEditor.vue')
  assert.match(editor, /canvasRef\.value\?\.addResourceInputs\(batch\)/)
})

test('compact resource selection has one shared input and tree owner across System authorization views', () => {
  const select = source('common-frontend/basic/src/components/ResourceTreeSelect.vue')
  assert.match(select, /<ResourceTreePicker\s/)
  assert.doesNotMatch(select, /<el-tree\b|<ResourceTree\s|fetch\(|axios/)
  assert.match(select, /@keydown\.esc\.stop="close"/)
  assert.match(select, /@keydown\.down\.prevent="browse"/)
  assert.match(source('common-frontend/basic/src/index.js'), /export .*ResourceTreeSelect/)
  for (const file of ['EngineDataAuthorization.vue', 'EngineSourceGrants.vue']) {
    const consumer = source(`system/frontend/src/components/engines/${file}`)
    assert.match(consumer, /<ResourceTreeSelect\s/)
    assert.doesNotMatch(consumer, /<ResourceTreePicker\s|<el-popover\b|<el-tree\b/)
  }
})

test('manager resource-selection forms compose ResourceTreePicker instead of rebuilding resource trees', () => {
  const consumers = [
    'manager/frontend/src/components/tasks/VectorTileSetTaskEditor.vue',
    'manager/frontend/src/components/tasks/RasterMosaicTaskEditor.vue',
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

test('System permission editing composes the shared tree without becoming another resource picker', () => {
  const editor = source('system/frontend/src/components/engines/EngineDirectPermissions.vue')
  const treeTag = editor.match(/<ResourceTree\s[\s\S]*?>/)?.[0]

  // The checkbox edits effective permission state, not a locator selection.
  assert.match(editor, /import \{ ResourceTree, filterTree \} from '@common-ui'/)
  assert.equal((editor.match(/<ResourceTree(?:\s|>)/g) || []).length, 1)
  assert.match(editor, /<template #node="\{ data \}">/)
  assert.match(editor, /:model-value="checked\(data\)"/)
  assert.match(editor, /:indeterminate="mixed\(data\)"/)
  assert.match(editor, /permissionNodeItems\(originalNodes\.value\.get\(node\.id\)/)
  assert.match(editor, /filterTree\(tree\.value,/)
  assert.match(treeTag, /:tree-data="visibleTree"/)
  assert.match(treeTag, /:filter-text="query"/)
  assert.match(treeTag, /:filter-method="matchesQuery"/)

  // Rendering, search input, expansion and generic resource selection stay shared.
  assert.doesNotMatch(editor, /<el-tree\b|ResourceTreePicker|ResourceTreeSelect|defineEmits\(/)
  assert.doesNotMatch(editor, /<el-input\b[^>]*v-model="query"/)
  assert.doesNotMatch(editor, /expandedKeys|@node-(?:expand|collapse|dblclick)|:lazy=|:load=/)
  assert.doesNotMatch(editor, /dataExplorerAPI|getResourceTree|createEngineCatalogTreeAdapter/)
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
    'orchestrator/frontend/src/components/TaskPanel.vue',
    'system/frontend/src/components/engines/EngineDirectPermissions.vue'
  ])
})

test('manager views do not introduce another form-level resource tree implementation', () => {
  for (const filePath of vueFilesUnder('manager/frontend/src/views')) {
    const content = readFileSync(filePath, 'utf8')
    assert.doesNotMatch(content, /<ResourceTree(?:\s|>)/, filePath)
    assert.doesNotMatch(content, /dataExplorerAPI\.get(?:Tree|NodeChildren|TreeAncestors)\(/, filePath)
  }
})
