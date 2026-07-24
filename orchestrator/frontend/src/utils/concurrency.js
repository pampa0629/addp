export function createConcurrencyLimiter(concurrency) {
  if (!Number.isInteger(concurrency) || concurrency < 1) {
    throw new TypeError('concurrency must be a positive integer')
  }

  const queue = []
  let activeCount = 0

  function drain() {
    while (activeCount < concurrency && queue.length > 0) {
      const { task, resolve, reject } = queue.shift()
      activeCount += 1
      Promise.resolve()
        .then(task)
        .then(resolve, reject)
        .finally(() => {
          activeCount -= 1
          drain()
        })
    }
  }

  return function limit(task) {
    if (typeof task !== 'function') {
      return Promise.reject(new TypeError('task must be a function'))
    }

    return new Promise((resolve, reject) => {
      queue.push({ task, resolve, reject })
      drain()
    })
  }
}
