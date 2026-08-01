import assert from 'node:assert/strict'

import {
  buildDevelopTaskEditorLocation,
  buildDevelopTaskPageLocation,
  developTaskIDFromRoute,
  normalizeDevelopTaskID
} from '../src/utils/developTaskRoute.js'

assert.deepEqual(buildDevelopTaskPageLocation('script'), { path: '/notebook' })
assert.deepEqual(buildDevelopTaskEditorLocation('query'), {
  path: '/sql',
  query: { action: 'create' }
})
assert.deepEqual(buildDevelopTaskEditorLocation('workflow', 544), {
  path: '/workflow',
  query: { action: 'edit', id: '544' }
})
assert.deepEqual(buildDevelopTaskEditorLocation('script', '12'), {
  path: '/notebook',
  query: { action: 'edit', id: '12' }
})
assert.equal(developTaskIDFromRoute({ query: { id: ['21'] } }), '21')
assert.equal(developTaskIDFromRoute({ query: { taskId: '21' } }), '')
assert.throws(() => normalizeDevelopTaskID('abc'), /positive integer/)
assert.throws(() => buildDevelopTaskEditorLocation('legacy', 1), /unsupported Develop task type/)

console.log('developTaskRoute tests passed')
