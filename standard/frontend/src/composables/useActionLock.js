import { nextTick, reactive } from 'vue'

export function useActionLock() {
  const lockedKeys = reactive(new Set())

  const isLocked = (key) => lockedKeys.has(key)

  const runLocked = async (key, action) => {
    if (lockedKeys.has(key)) return
    const returnFocus = typeof document === 'undefined' ? null : document.activeElement
    lockedKeys.add(key)
    try {
      return await action()
    } finally {
      lockedKeys.delete(key)
      await nextTick()
      if (returnFocus?.isConnected) returnFocus.focus()
    }
  }

  return { isLocked, runLocked }
}
