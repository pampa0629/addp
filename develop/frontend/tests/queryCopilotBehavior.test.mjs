import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const editor = await readFile(resolve('src/views/QueryEditor.vue'), 'utf8')
const api = await readFile(resolve('src/api/copilot.js'), 'utf8')

assert.match(api, /generateQueryFromNL/)
assert.match(api, /\/copilot\/query\/generate/)
assert.doesNotMatch(api, /\/copilot\/sql\/generate/)
assert.match(editor, /generateQueryFromNL\(\{[\s\S]*query:[\s\S]*engine_id:[\s\S]*query_language:[\s\S]*resources/)
assert.match(editor, /selectedEngineId\.value/)
assert.match(editor, /currentQueryLanguage\.value/)
assert.match(editor, /collectSelectedQueryResources/)
assert.match(editor, /confirmedResources\(/)
assert.match(editor, /queryContent\.value = resolved\.query/)
assert.doesNotMatch(editor, /submitQuery\(\{\}\)[\s\S]{0,200}resolved\.query/)
assert.match(editor, /class="query-ai-fab"/)
assert.match(editor, /v-model="queryResourceConfirmationVisible"/)

console.log('queryCopilotBehavior tests passed')
