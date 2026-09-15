import { onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { resolveConsoleOrigin } from '../utils/consoleOrigin'

export const UNSAVED_CHANGES_MESSAGE = 'addp:unsaved-changes'

// One dialog per navigation owner, even when several navigation attempts overlap.
function createLeaveConfirmation(t) {
  let pending
  return () => {
    if (!pending) {
      pending = ElMessageBox.confirm(t('unsavedChanges.message'), t('unsavedChanges.title'), {
        type: 'warning', customClass: 'addp-message-box',
        confirmButtonText: t('unsavedChanges.leave'),
        cancelButtonText: t('unsavedChanges.stay'),
        confirmButtonClass: 'el-button--danger',
        closeOnClickModal: false, closeOnHashChange: false
      }).then(() => true, () => false).finally(() => { pending = undefined })
    }
    return pending
  }
}

function useUnloadProtection(isDirty) {
  const handleBeforeUnload = event => {
    if (!isDirty()) return
    event.preventDefault()
    event.returnValue = ''
  }
  window.addEventListener('beforeunload', handleBeforeUnload)
  onBeforeUnmount(() => window.removeEventListener('beforeunload', handleBeforeUnload))
}

export function useUnsavedChangesGuard({ router, isDirty, shouldConfirmUpdate = () => true }) {
  const { t } = useI18n()
  const confirmLeave = createLeaveConfirmation(t)
  const guard = () => isDirty() ? confirmLeave() : true
  const removeGuard = router.beforeEach((to, from) => {
    if (to.fullPath === from.fullPath) return true
    const samePage = to.matched.at(-1) === from.matched.at(-1)
    return samePage && !shouldConfirmUpdate(to, from) ? true : guard()
  })
  onBeforeUnmount(removeGuard)
  useUnloadProtection(isDirty)

  if (window.parent !== window) {
    const id = crypto.randomUUID()
    const origin = resolveConsoleOrigin(window.location)
    const publish = (active = true) => window.parent.postMessage({
      type: UNSAVED_CHANGES_MESSAGE, id, active, dirty: active && Boolean(isDirty())
    }, origin)
    const handleRequest = event => {
      if (event.source === window.parent && event.origin === origin &&
          event.data?.type === UNSAVED_CHANGES_MESSAGE && event.data.request === true) publish()
    }
    window.addEventListener('message', handleRequest)
    watch(isDirty, () => publish(), { immediate: true, flush: 'sync' })
    onBeforeUnmount(() => {
      publish(false)
      window.removeEventListener('message', handleRequest)
    })
  }
}

export function useConsoleUnsavedChangesGuard(router, { getIframe, skipNavigation = () => false }) {
  const { t } = useI18n()
  const confirmLeave = createLeaveConfirmation(t)
  let current = null
  const activeState = () => current?.frame === getIframe()?.contentWindow ? current : null
  const isDirty = () => Boolean(activeState()?.dirty)
  const receive = event => {
    const iframe = getIframe()
    if (!iframe || event.source !== iframe.contentWindow || event.origin !== new URL(iframe.src).origin) return
    const message = event.data
    if (message?.type !== UNSAVED_CHANGES_MESSAGE || typeof message.id !== 'string' ||
        typeof message.active !== 'boolean' || typeof message.dirty !== 'boolean') return
    if (message.active) current = { id: message.id, dirty: message.dirty, frame: event.source }
    else if (activeState()?.id === message.id) current = null
  }
  window.addEventListener('message', receive)
  const removeGuard = router.beforeEach(async (to, from) => {
    if (to.fullPath === from.fullPath || skipNavigation(to) || !isDirty()) return true
    return confirmLeave()
  })
  useUnloadProtection(isDirty)
  onBeforeUnmount(() => {
    removeGuard()
    window.removeEventListener('message', receive)
    current = null
  })
  return {
    reset() { current = null },
    requestState() {
      const iframe = getIframe()
      iframe?.contentWindow?.postMessage({ type: UNSAVED_CHANGES_MESSAGE, request: true }, new URL(iframe.src).origin)
    }
  }
}
