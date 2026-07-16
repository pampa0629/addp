import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildWebhookDestinationPayload,
  canRetryNotificationDelivery,
  notificationDeliveryTagType
} from '../src/utils/notification.js'

test('omits an empty secret when updating an existing webhook destination', () => {
  assert.deepEqual(buildWebhookDestinationPayload({
    name: ' ops ', url: ' https://ops.example.com/hook ', secret: '', enabled: true,
    event_types: ['opened', 'resolved']
  }, true), {
    name: 'ops', url: 'https://ops.example.com/hook', enabled: true,
    event_types: ['opened', 'resolved']
  })
})

test('includes the signing secret when creating a webhook destination', () => {
  const payload = buildWebhookDestinationPayload({
    name: 'ops', url: 'https://ops.example.com/hook', secret: '0123456789abcdef', enabled: true,
    event_types: ['opened']
  })
  assert.equal(payload.secret, '0123456789abcdef')
})

test('maps terminal delivery failures to the danger tag', () => {
  assert.equal(notificationDeliveryTagType('dead'), 'danger')
  assert.equal(notificationDeliveryTagType('delivered'), 'success')
})

test('only allows manual retry for dead deliveries', () => {
  assert.equal(canRetryNotificationDelivery({ status: 'dead' }), true)
  assert.equal(canRetryNotificationDelivery({ status: 'pending' }), false)
  assert.equal(canRetryNotificationDelivery({ status: 'delivered' }), false)
})
