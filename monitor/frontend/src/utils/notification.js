export const NOTIFICATION_EVENT_TYPES = ['opened', 'escalated', 'resolved']

export function buildWebhookDestinationPayload(form, editing = false) {
  const payload = {
    name: form.name.trim(),
    url: form.url.trim(),
    enabled: Boolean(form.enabled),
    event_types: [...form.event_types]
  }
  const secret = form.secret.trim()
  if (!editing || secret) payload.secret = secret
  return payload
}

export function buildEmailDestinationPayload(form) {
  const recipients = [...new Map(
    form.recipients
      .map(value => value.trim())
      .filter(Boolean)
      .map(value => [value.toLowerCase(), value])
  ).values()].sort((left, right) => left.localeCompare(right))
  return {
    name: form.name.trim(),
    recipients,
    enabled: Boolean(form.enabled),
    event_types: [...form.event_types]
  }
}

export function notificationDeliveryTagType(status) {
  const types = {
    pending: 'warning',
    delivering: 'primary',
    delivered: 'success',
    dead: 'danger',
    suppressed: 'info',
    cancelled: 'info'
  }
  return types[status] || 'info'
}

export function canRetryNotificationDelivery(delivery) {
  return delivery?.status === 'dead'
}
