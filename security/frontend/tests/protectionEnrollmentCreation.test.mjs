import { nextTick, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionEnrollmentCreation } from '../src/composables/useProtectionEnrollmentCreation.mjs'

const selection = {
  identity: { item_id: 7, locator: 'addp://engine/1/path/public/customers?type=table&item_id=7' },
  display: { engine_name: 'Business PostgreSQL' }
}

const item = {
  id: 7,
  name: 'customers',
  full_name: 'business.customers',
  fingerprint: 'resource-fingerprint-7'
}

function createSession(overrides = {}) {
  const dependencies = {
    enrollments: ref([]),
    getItem: vi.fn().mockResolvedValue(item),
    createEnrollment: vi.fn().mockResolvedValue({ id: 11, state: 'enrolling' }),
    showCurrentCollection: vi.fn(),
    refreshCollection: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    t: vi.fn(key => key),
    onItemLoadError: vi.fn(),
    onAlreadyEnrolled: vi.fn(),
    onCreated: vi.fn(),
    onSubmitError: vi.fn(),
    onClosed: vi.fn().mockResolvedValue(undefined),
    ...overrides
  }
  return {
    dependencies,
    session: useProtectionEnrollmentCreation(dependencies)
  }
}

function prepareValidSelection(session) {
  session.form.resource = selection
  session.selectedItem.value = item
  session.drawerRef.value = {
    validate: vi.fn().mockResolvedValue(true),
    validateField: vi.fn().mockResolvedValue(true),
    clearValidate: vi.fn()
  }
}

describe('protection enrollment creation session', () => {
  it('opens a clean session and owns the required resource rule', () => {
    const { session } = createSession()
    session.form.resource = selection
    session.selectedItem.value = item

    expect(session.open(' addp://engine/1/path/public/customers?type=table ')).toBe(true)
    expect(session.drawer.value).toBe(true)
    expect(session.initialLocator.value).toBe('addp://engine/1/path/public/customers?type=table')
    expect(session.form.resource).toBeNull()
    expect(session.selectedItem.value).toBeNull()

    const callback = vi.fn()
    session.formRules.value.resource[0].validator({}, null, callback)
    expect(callback).toHaveBeenCalledWith(expect.any(Error))
  })

  it('loads the selected Meta item, validates it inline, and detects an existing lifecycle', async () => {
    const enrollments = ref([{ id: 5, state: 'active', target: { resource_identity: item.fingerprint } }])
    const { dependencies, session } = createSession({ enrollments })
    session.drawerRef.value = { validateField: vi.fn().mockResolvedValue(true) }

    await expect(session.selectResource(selection)).resolves.toEqual({ status: 'selected', item })
    await nextTick()

    expect(dependencies.getItem).toHaveBeenCalledWith(7)
    expect(session.selectedItem.value).toEqual(item)
    expect(session.existingEnrollment.value).toEqual(enrollments.value[0])
    expect(session.drawerRef.value.validateField).toHaveBeenCalledWith('resource')

    const callback = vi.fn()
    session.formRules.value.resource[0].validator({}, selection, callback)
    expect(callback).toHaveBeenCalledWith()
  })

  it('ignores a slower item response after a newer resource is selected', async () => {
    const pending = new Map()
    const getItem = vi.fn(id => new Promise(resolve => pending.set(id, resolve)))
    const { session } = createSession({ getItem })
    session.drawerRef.value = { validateField: vi.fn().mockResolvedValue(true) }
    const secondSelection = {
      identity: { item_id: 8, locator: 'addp://engine/1/path/public/orders?type=table&item_id=8' }
    }
    const secondItem = { ...item, id: 8, name: 'orders', fingerprint: 'resource-fingerprint-8' }

    const first = session.selectResource(selection)
    const second = session.selectResource(secondSelection)
    pending.get(8)(secondItem)
    await expect(second).resolves.toEqual({ status: 'selected', item: secondItem })
    pending.get(7)(item)
    await expect(first).resolves.toEqual({ status: 'stale' })

    expect(session.selectedItem.value).toEqual(secondItem)
    expect(session.selectedItemLoading.value).toBe(false)
  })

  it('clears an invalid selection and reports item load failures', async () => {
    const loadError = new Error('item load failed')
    const { dependencies, session } = createSession({ getItem: vi.fn().mockRejectedValue(loadError) })

    await expect(session.selectResource(selection)).resolves.toEqual({ status: 'failed', error: loadError })
    expect(dependencies.onItemLoadError).toHaveBeenCalledWith(loadError)
    expect(session.selectedItem.value).toBeNull()

    session.form.resource = selection
    expect(session.updateResource(null)).toBe(true)
    expect(session.selectedItem.value).toBeNull()
    expect(session.selectedItemLoading.value).toBe(false)
  })

  it('rejects an already enrolled resource without calling create', async () => {
    const enrollments = ref([{ id: 5, state: 'active', target: { resource_identity: item.fingerprint } }])
    const { dependencies, session } = createSession({ enrollments })
    session.open()
    prepareValidSelection(session)

    await expect(session.submit()).resolves.toMatchObject({ status: 'already_enrolled', enrollment: enrollments.value[0] })
    expect(dependencies.createEnrollment).not.toHaveBeenCalled()
    expect(dependencies.onAlreadyEnrolled).toHaveBeenCalledOnce()
    expect(session.saving.value).toBe(false)
  })

  it('locks before validation and creates one enrollment from the validated locator', async () => {
    let resolveValidation
    const { dependencies, session } = createSession()
    session.open()
    prepareValidSelection(session)
    session.drawerRef.value.validate = vi.fn(() => new Promise(resolve => { resolveValidation = resolve }))

    const first = session.submit()
    const second = session.submit()
    expect(session.saving.value).toBe(true)
    expect(session.requestClose()).toBe(false)
    const closeDrawer = vi.fn()
    expect(session.beforeClose(closeDrawer)).toBe(false)
    expect(closeDrawer).not.toHaveBeenCalled()
    resolveValidation(true)

    await expect(first).resolves.toMatchObject({ status: 'created' })
    await expect(second).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.createEnrollment).toHaveBeenCalledOnce()
    expect(dependencies.createEnrollment).toHaveBeenCalledWith({ locator: selection.identity.locator })
    expect(dependencies.showCurrentCollection).toHaveBeenCalledOnce()
    expect(dependencies.refreshCollection).toHaveBeenCalledOnce()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onCreated).toHaveBeenCalledOnce()
    expect(session.drawer.value).toBe(false)
  })

  it('does not create from a resource selection changed during validation', async () => {
    let resolveValidation
    const { dependencies, session } = createSession()
    session.open()
    prepareValidSelection(session)
    session.drawerRef.value.validate = vi.fn(() => new Promise(resolve => { resolveValidation = resolve }))

    const submission = session.submit()
    session.updateResource(null)
    resolveValidation(true)

    await expect(submission).resolves.toEqual({ status: 'invalid' })
    expect(dependencies.createEnrollment).not.toHaveBeenCalled()
    expect(session.saving.value).toBe(false)
  })

  it('reports create failures and keeps the validated session open', async () => {
    const submitError = new Error('create failed')
    const { dependencies, session } = createSession({
      createEnrollment: vi.fn().mockRejectedValue(submitError)
    })
    session.open()
    prepareValidSelection(session)

    await expect(session.submit()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(dependencies.onSubmitError).toHaveBeenCalledWith(submitError)
    expect(session.drawer.value).toBe(true)
    expect(session.form.resource).toEqual(selection)
    expect(session.saving.value).toBe(false)
  })

  it('keeps invalid forms open and invalidates pending work on close or disposal', async () => {
    const invalid = createSession()
    invalid.session.open()
    prepareValidSelection(invalid.session)
    invalid.session.drawerRef.value.validate = vi.fn().mockResolvedValue(false)
    await expect(invalid.session.submit()).resolves.toEqual({ status: 'invalid' })
    expect(invalid.dependencies.createEnrollment).not.toHaveBeenCalled()
    expect(invalid.session.drawer.value).toBe(true)

    let resolveItem
    const pending = createSession({ getItem: vi.fn(() => new Promise(resolve => { resolveItem = resolve })) })
    pending.session.open()
    const selectionLoad = pending.session.selectResource(selection)
    expect(pending.session.requestClose()).toBe(true)
    resolveItem(item)
    await expect(selectionLoad).resolves.toEqual({ status: 'stale' })
    await pending.session.closed()
    expect(pending.session.selectedItem.value).toBeNull()
    expect(pending.dependencies.onClosed).toHaveBeenCalledOnce()

    let resolveCreate
    const disposed = createSession({ createEnrollment: vi.fn(() => new Promise(resolve => { resolveCreate = resolve })) })
    disposed.session.open()
    prepareValidSelection(disposed.session)
    const submission = disposed.session.submit()
    await vi.waitFor(() => expect(disposed.dependencies.createEnrollment).toHaveBeenCalledOnce())
    disposed.session.dispose()
    resolveCreate({ id: 12 })
    await expect(submission).resolves.toEqual({ status: 'stale' })
    expect(disposed.dependencies.refreshCollection).not.toHaveBeenCalled()
  })
})
