import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const executionListSource = readFileSync(resolve('src/views/ExecutionList.vue'), 'utf8')

test('execution route filters restore the complete task scope without clearing task type', () => {
  assert.match(executionListSource, /const nextSourceTaskID = firstQueryValue\(query\.source_task_id\)/)
  assert.match(executionListSource, /filters\.value\.source_task_id = nextSourceTaskID/)
  assert.match(executionListSource, /v-model="filters\.module"[\s\S]*@change="handleModuleChange"/)
  assert.doesNotMatch(executionListSource, /watch\(\s*\(\) => filters\.value\.module/)
})
