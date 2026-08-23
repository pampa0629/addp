const TRANSIENT_HTTP_STATUS = new Set([502, 503, 504])
const TRANSIENT_NETWORK_CODES = new Set(['ECONNABORTED', 'ERR_NETWORK', 'ETIMEDOUT'])

export const DEFAULT_TRANSIENT_RETRY_DELAYS_MS = Object.freeze([
  500,
  1000,
  2000,
  3000,
  4000,
  5000,
  6000,
  7000
])

export const isTransientRequestError = error => {
  const status = Number(error?.response?.status || error?.status || 0)
  if (TRANSIENT_HTTP_STATUS.has(status)) {
    return true
  }
  return !error?.response && TRANSIENT_NETWORK_CODES.has(String(error?.code || '').toUpperCase())
}

const waitForDelay = delay => new Promise(resolve => setTimeout(resolve, delay))

export async function withTransientRetry(operation, options = {}) {
  const delays = options.delays || DEFAULT_TRANSIENT_RETRY_DELAYS_MS
  const shouldRetry = options.shouldRetry || isTransientRequestError
  const wait = options.wait || waitForDelay

  for (let attempt = 0; ; attempt += 1) {
    try {
      return await operation()
    } catch (error) {
      if (attempt >= delays.length || !shouldRetry(error)) {
        throw error
      }
      await wait(delays[attempt], { attempt: attempt + 1, error })
    }
  }
}
