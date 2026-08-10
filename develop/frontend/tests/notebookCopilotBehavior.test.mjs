import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const editor = await readFile(resolve('src/views/NotebookEditor.vue'), 'utf8')
const api = await readFile(resolve('src/api/notebook.js'), 'utf8')

assert.match(api, /generateSessionCell\(sessionId, payload\)/)
assert.match(api, /\/develop\/notebook-copilot-sessions\/\$\{sessionId\}\/generate/)
assert.match(editor, /class="notebook-ai-fab"/)
assert.match(editor, /class="notebook-ai-panel"/)
assert.doesNotMatch(editor, /<el-drawer[\s\S]*copilot/)
assert.match(editor, /<el-tooltip[\s\S]*:disabled="copilotVisible"[\s\S]*class="notebook-ai-fab"/)
assert.doesNotMatch(editor, /@closed="resetNotebookCopilot"/)
assert.match(editor, /const closeNotebookSession = async \(\) => \{[\s\S]*resetNotebookCopilot\(\)/)
assert.match(editor, /copilotRoles\.value\.find\(role => !copilotSelections\.value\[role\]\)/)
assert.match(editor, /type: 'addp:notebook:insert-cell'/)

console.log('notebookCopilotBehavior tests passed')
