export function createLatestOperationController() {
  let sequence = 0
  let current = null

  const isCurrent = operation => current?.id === operation.id

  async function execute(kind, request, handlers = {}) {
    current?.controller.abort()
    const operation = {
      id: ++sequence,
      kind,
      controller: new AbortController(),
    }
    current = operation
    handlers.onStart?.(operation)

    try {
      const value = await request(operation.controller.signal)
      if (!isCurrent(operation)) return false
      await handlers.onSuccess?.(value, operation)
      return true
    } catch (error) {
      if (!isCurrent(operation) || isCanceledRequest(error, operation.controller.signal)) return false
      if (handlers.onError) {
        await handlers.onError(error, operation)
        return false
      }
      throw error
    } finally {
      if (isCurrent(operation)) {
        current = null
        handlers.onFinish?.(operation)
      }
    }
  }

  function cancel() {
    current?.controller.abort()
    current = null
  }

  return { execute, cancel }
}

export function isCanceledRequest(error, signal) {
  return signal?.aborted || error?.name === 'AbortError' || error?.code === 'ERR_CANCELED'
}
