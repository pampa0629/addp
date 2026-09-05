import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const repositoryRoot = resolve(import.meta.dirname, '../../..')
const source = relativePath => readFileSync(resolve(repositoryRoot, relativePath), 'utf8')

test('execution detail delegates structured lineage rendering to its single component', () => {
  const executionList = source('monitor/frontend/src/views/ExecutionList.vue')
  const component = source('monitor/frontend/src/components/ExecutionLineageSummary.vue')

  assert.match(executionList, /import ExecutionLineageSummary from '@\/components\/ExecutionLineageSummary\.vue'/)
  assert.match(executionList, /<ExecutionLineageSummary :metadata="currentExecutionMetadata"\s*\/?>/)
  assert.doesNotMatch(executionList, /lineage_facts\.(?:inputs|outputs)/)
  assert.match(component, /buildExecutionLineageSummary/)
  assert.match(component, /buildManagerDataExplorerRoute/)
  assert.match(component, /openConsoleRoute/)
  assert.match(component, /resource\.direction === 'input' && resource\.explorable/)
  assert.match(component, /monitor\.execution\.detail\.lineage\.open_in_data_explorer/)
  assert.match(component, /monitor\.execution\.detail\.lineage\.inputs/)
  assert.match(component, /monitor\.execution\.detail\.lineage\.outputs/)
  assert.match(component, /resource\.platformInternal/)
  assert.match(component, /monitor\.execution\.detail\.lineage\.platform_internal_artifact/)
  assert.doesNotMatch(component, /temporary_artifact/)
})

test('raw execution metadata is collapsed by default and resets between executions', () => {
  const executionList = source('monitor/frontend/src/views/ExecutionList.vue')

  assert.match(executionList, /const metadataExpandedPanels = ref\(\[\]\)/)
  assert.match(executionList, /<el-collapse v-model="metadataExpandedPanels"/)
  assert.match(executionList, /monitor\.execution\.detail\.raw_metadata/)
  assert.match(
    executionList,
    /\(\) => currentExecution\.value\?\.execution_id,[\s\S]*?metadataExpandedPanels\.value = \[\]/
  )
})
