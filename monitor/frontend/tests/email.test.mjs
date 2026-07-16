import test from 'node:test'
import assert from 'node:assert/strict'

import { buildEmailDestinationPayload } from '../src/utils/notification.js'

test('normalizes and deduplicates email recipients', () => {
  assert.deepEqual(buildEmailDestinationPayload({
    name: ' on-call ', recipients: [' OPS@example.com ', 'ops@example.com', 'other@example.com'],
    enabled: true, event_types: ['opened', 'resolved']
  }), {
    name: 'on-call', recipients: ['ops@example.com', 'other@example.com'],
    enabled: true, event_types: ['opened', 'resolved']
  })
})
