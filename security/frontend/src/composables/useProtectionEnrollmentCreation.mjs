import { computed, nextTick, reactive, ref, unref } from 'vue'

function resourceLocator(selection) {
  return String(selection?.identity?.locator || '').trim()
}

function resourceItemID(selection) {
  return Number(selection?.identity?.item_id || 0)
}

export function useProtectionEnrollmentCreation({
  enrollments,
  getItem,
  createEnrollment,
  showCurrentCollection,
  refreshCollection,
  scheduleAutoRefresh,
  t,
  onItemLoadError = () => {},
  onAlreadyEnrolled = () => {},
  onCreated = () => {},
  onSubmitError = () => {},
  onClosed = async () => {}
}) {
  const drawer = ref(false)
  const drawerRef = ref(null)
  const form = reactive({ resource: null })
  const selectedItem = ref(null)
  const selectedItemLoading = ref(false)
  const initialLocator = ref('')
  const saving = ref(false)

  let itemRequest = 0
  let submitRequest = 0
  let disposed = false

  const existingEnrollment = computed(() => {
    const fingerprint = String(selectedItem.value?.fingerprint || '')
    if (!fingerprint) return null
    const currentEnrollments = unref(enrollments)
    const rows = Array.isArray(currentEnrollments) ? currentEnrollments : []
    return rows.find(row => row.target?.resource_identity === fingerprint && row.state !== 'released') || null
  })

  const formRules = computed(() => ({
    resource: [{
      validator: (_rule, selection, callback) => {
        const locator = resourceLocator(selection)
        const itemID = resourceItemID(selection)
        if (locator && itemID > 0 && Number(selectedItem.value?.id) === itemID) {
          callback()
          return
        }
        callback(new Error(t('security.enrollment.resourceRequired')))
      },
      trigger: 'change'
    }]
  }))

  function clearSession() {
    itemRequest += 1
    drawer.value = false
    form.resource = null
    selectedItem.value = null
    selectedItemLoading.value = false
    initialLocator.value = ''
    saving.value = false
  }

  function open(locator = '') {
    if (disposed || saving.value) return false
    itemRequest += 1
    submitRequest += 1
    clearSession()
    initialLocator.value = String(locator || '').trim()
    drawer.value = true
    return true
  }

  function updateResource(selection) {
    form.resource = selection || null
    if (resourceLocator(selection)) return false
    itemRequest += 1
    selectedItem.value = null
    selectedItemLoading.value = false
    return true
  }

  async function selectResource(selection) {
    if (disposed) return { status: 'unavailable' }
    const request = ++itemRequest
    form.resource = selection || null
    selectedItem.value = null
    const itemID = resourceItemID(selection)
    if (!itemID || !resourceLocator(selection)) {
      selectedItemLoading.value = false
      return { status: 'invalid' }
    }

    selectedItemLoading.value = true
    try {
      const item = await getItem(itemID)
      if (request !== itemRequest || disposed) return { status: 'stale' }
      selectedItem.value = item
      await nextTick()
      if (request !== itemRequest || disposed) return { status: 'stale' }
      await drawerRef.value?.validateField?.('resource').catch(() => false)
      return { status: 'selected', item }
    } catch (error) {
      if (request !== itemRequest || disposed) return { status: 'stale' }
      onItemLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === itemRequest) selectedItemLoading.value = false
    }
  }

  function requestClose() {
    if (saving.value) return false
    itemRequest += 1
    submitRequest += 1
    drawer.value = false
    return true
  }

  function beforeClose(done) {
    if (saving.value) return false
    itemRequest += 1
    submitRequest += 1
    done()
    return true
  }

  async function closed() {
    if (disposed) return false
    submitRequest += 1
    clearSession()
    await onClosed()
    return true
  }

  async function submit() {
    if (disposed || saving.value || selectedItemLoading.value) return { status: 'unavailable' }
    const request = ++submitRequest
    saving.value = true
    const validation = drawerRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== submitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      saving.value = false
      return { status: 'invalid' }
    }

    const locator = resourceLocator(form.resource)
    const itemID = resourceItemID(form.resource)
    if (!locator || itemID <= 0 || Number(selectedItem.value?.id) !== itemID) {
      saving.value = false
      return { status: 'invalid' }
    }
    const duplicate = existingEnrollment.value
    if (duplicate) {
      saving.value = false
      onAlreadyEnrolled(duplicate)
      return { status: 'already_enrolled', enrollment: duplicate }
    }

    try {
      const result = await createEnrollment({ locator })
      if (request !== submitRequest || disposed) return { status: 'stale' }
      showCurrentCollection()
      await refreshCollection()
      if (request !== submitRequest || disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      clearSession()
      onCreated()
      return { status: 'created', result }
    } catch (error) {
      if (request !== submitRequest || disposed) return { status: 'stale' }
      onSubmitError(error)
      return { status: 'failed', error }
    } finally {
      if (request === submitRequest) saving.value = false
    }
  }

  function dispose() {
    disposed = true
    itemRequest += 1
    submitRequest += 1
    selectedItemLoading.value = false
    saving.value = false
  }

  return {
    drawer,
    drawerRef,
    form,
    formRules,
    selectedItem,
    selectedItemLoading,
    initialLocator,
    existingEnrollment,
    saving,
    open,
    updateResource,
    selectResource,
    requestClose,
    beforeClose,
    closed,
    submit,
    dispose
  }
}
