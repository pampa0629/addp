import assert from 'node:assert/strict'
import {
  engineSelectionState,
  isEngineSelectable
} from '../../../common-frontend/basic/src/utils/engineAvailability.js'

const online = { lifecycle_state: 'active', connection_status: 'online' }
assert.equal(isEngineSelectable(online), true)
assert.equal(engineSelectionState(online), 'available')

for (const [connectionStatus, state] of [
  ['offline', 'offline'],
  ['unknown', 'unknown'],
  ['checking', 'checking']
]) {
  const engine = { lifecycle_state: 'active', connection_status: connectionStatus }
  assert.equal(isEngineSelectable(engine), false)
  assert.equal(engineSelectionState(engine), state)
}

assert.equal(isEngineSelectable({ lifecycle_state: 'disabled', connection_status: 'online' }), false)
assert.equal(engineSelectionState({ lifecycle_state: 'disabled', connection_status: 'online' }), 'disabled')
assert.equal(engineSelectionState(null), 'missing')

