const CONSOLE_BRIDGE_REQUEST = 'addp:console-bridge:request'
const CONSOLE_BRIDGE_RESPONSE = 'addp:console-bridge:response'

const defaultRequestId = () => `${Date.now()}-${Math.random().toString(36).slice(2)}`

export function toConsoleBridgeValue(value, seen = new WeakMap()) {
  if (value === null || value === undefined) return value

  const valueType = typeof value
  if (valueType === 'string' || valueType === 'number' || valueType === 'boolean' || valueType === 'bigint') {
    return value
  }
  if (valueType === 'function' || valueType === 'symbol') {
    throw new TypeError(`Console bridge payload contains unsupported ${valueType} value`)
  }

  if (seen.has(value)) return seen.get(value)

  if (Array.isArray(value)) {
    const result = []
    seen.set(value, result)
    value.forEach(item => result.push(toConsoleBridgeValue(item, seen)))
    return result
  }

  const prototype = Object.getPrototypeOf(value)
  if (prototype !== Object.prototype && prototype !== null) {
    throw new TypeError('Console bridge payload must contain only plain objects and arrays')
  }

  const result = {}
  seen.set(value, result)
  Object.keys(value).forEach(key => {
    result[key] = toConsoleBridgeValue(value[key], seen)
  })
  return result
}

export function requestConsoleBridge(channel, payload, options = {}) {
  const {
    source = 'addp-module',
    timeout = 15000,
    targetOrigin = '*',
    timeoutMessage = 'Console bridge request timed out'
  } = options

  if (!channel) {
    return Promise.reject(new Error('console bridge channel is required'))
  }
  if (typeof window === 'undefined' || window.parent === window) {
    return Promise.reject(new Error('Console bridge requires an iframe parent'))
  }

  const requestId = defaultRequestId()
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => {
      window.removeEventListener('message', handleMessage)
      reject(new Error(timeoutMessage))
    }, timeout)

    function handleMessage(event) {
      const message = event.data
      if (
        !message ||
        message.type !== CONSOLE_BRIDGE_RESPONSE ||
        message.channel !== channel ||
        message.requestId !== requestId
      ) {
        return
      }

      window.clearTimeout(timer)
      window.removeEventListener('message', handleMessage)
      if (message.ok) {
        resolve(message.data)
      } else {
        reject(new Error(message.error || 'Console bridge request failed'))
      }
    }

    window.addEventListener('message', handleMessage)
    window.parent.postMessage({
      type: CONSOLE_BRIDGE_REQUEST,
      source,
      channel,
      requestId,
      payload: toConsoleBridgeValue(payload)
    }, targetOrigin)
  })
}

export function registerConsoleBridgeHandler(channel, handler, options = {}) {
  const {
    source = 'addp-console',
    allowedSources = []
  } = options

  if (!channel) {
    throw new Error('console bridge channel is required')
  }
  if (typeof window === 'undefined') {
    return () => {}
  }

  const allowed = new Set(allowedSources)

  async function handleMessage(event) {
    const message = event.data
    if (!message || message.type !== CONSOLE_BRIDGE_REQUEST || message.channel !== channel) {
      return
    }
    if (allowed.size > 0 && !allowed.has(message.source)) {
      return
    }

    const reply = (payload) => {
      event.source?.postMessage({
        type: CONSOLE_BRIDGE_RESPONSE,
        source,
        channel,
        requestId: message.requestId,
        ...toConsoleBridgeValue(payload)
      }, event.origin || '*')
    }

    try {
      const data = await handler(message.payload, message, event)
      reply({ ok: true, data })
    } catch (error) {
      reply({
        ok: false,
        error: error.response?.data?.error || error.message || 'Console bridge request failed'
      })
    }
  }

  window.addEventListener('message', handleMessage)
  return () => window.removeEventListener('message', handleMessage)
}
