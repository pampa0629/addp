import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const source = await readFile(resolve('src/components/workflow/WorkflowDAGCanvas.vue'), 'utf8')

assert.match(source, /graph\.value\.on\('node:click', handleNodeClick\)/)
assert.doesNotMatch(source, /node:dblclick/)
assert.match(source, /if \(!isAddEdgeMode\.value\) \{\s*emitNodeSelection\(e\)/)

console.log('workflowCanvasInteraction tests passed')
