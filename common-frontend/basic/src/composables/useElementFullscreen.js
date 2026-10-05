import { onBeforeUnmount, onMounted, ref } from 'vue'

/** Native fullscreen for one component-owned element, including external/Esc exit. */
export function useElementFullscreen(host, { onError = () => {} } = {}) {
  const isFullscreen = ref(false)
  const pending = ref(false)
  let ownerDocument

  function synchronize() {
    isFullscreen.value = Boolean(host.value && ownerDocument?.fullscreenElement === host.value)
  }

  async function toggleFullscreen() {
    const element = host.value
    if (!element || pending.value) return
    pending.value = true
    try {
      const doc = element.ownerDocument
      if (doc.fullscreenElement === element) await doc.exitFullscreen()
      else await element.requestFullscreen()
      synchronize()
    } catch (error) {
      onError(error)
    } finally {
      pending.value = false
    }
  }

  onMounted(() => {
    ownerDocument = host.value?.ownerDocument
    ownerDocument?.addEventListener('fullscreenchange', synchronize)
    synchronize()
  })
  onBeforeUnmount(() => {
    ownerDocument?.removeEventListener('fullscreenchange', synchronize)
    if (host.value && ownerDocument?.fullscreenElement === host.value) {
      ownerDocument.exitFullscreen().catch(onError)
    }
  })

  return { isFullscreen, pending, toggleFullscreen }
}
