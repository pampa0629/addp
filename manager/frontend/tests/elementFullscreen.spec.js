import { beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowRef } from 'vue'
import { useElementFullscreen } from '../../../common-frontend/basic/src/composables/useElementFullscreen.js'

const lifecycle = vi.hoisted(() => ({ mounted: [], unmount: [] }))
vi.mock('vue', async importOriginal => ({
  ...await importOriginal(),
  onMounted: callback => lifecycle.mounted.push(callback),
  onBeforeUnmount: callback => lifecycle.unmount.push(callback)
}))

beforeEach(() => { lifecycle.mounted = []; lifecycle.unmount = [] })

function fixture() {
  const doc = new EventTarget()
  doc.fullscreenElement = null
  const change = element => { doc.fullscreenElement = element; doc.dispatchEvent(new Event('fullscreenchange')) }
  doc.exitFullscreen = vi.fn(async () => change(null))
  const host = { ownerDocument: doc, requestFullscreen: vi.fn(async () => change(host)) }
  const onError = vi.fn()
  const controller = useElementFullscreen(shallowRef(host), { onError })
  lifecycle.mounted.forEach(callback => callback())
  return { doc, host, change, onError, ...controller }
}

describe('shared element fullscreen lifecycle', () => {
  it('tracks entry and external/Esc exit, and only exits its own fullscreen element', async () => {
    const state = fixture()
    await state.toggleFullscreen()
    expect(state.isFullscreen.value).toBe(true)
    state.change(null)
    expect(state.isFullscreen.value).toBe(false)
    state.change({})
    await state.toggleFullscreen()
    expect(state.doc.exitFullscreen).not.toHaveBeenCalled()
    expect(state.doc.fullscreenElement).toBe(state.host)
    await state.toggleFullscreen()
    expect(state.isFullscreen.value).toBe(false)
    expect(state.doc.exitFullscreen).toHaveBeenCalledOnce()
  })

  it('prevents overlapping requests and reports a rejected browser permission without changing state', async () => {
    const state = fixture()
    let reject
    state.host.requestFullscreen.mockImplementation(() => new Promise((_resolve, rejectRequest) => { reject = rejectRequest }))
    const pending = state.toggleFullscreen()
    await state.toggleFullscreen()
    expect(state.host.requestFullscreen).toHaveBeenCalledOnce()
    expect(state.pending.value).toBe(true)
    const error = new Error('permission denied')
    reject(error)
    await pending
    expect(state.pending.value).toBe(false)
    expect(state.isFullscreen.value).toBe(false)
    expect(state.onError).toHaveBeenCalledWith(error)
  })

  it('removes the listener and exits the owned fullscreen on unmount', async () => {
    const state = fixture()
    await state.toggleFullscreen()
    lifecycle.unmount.forEach(callback => callback())
    await Promise.resolve()
    expect(state.doc.exitFullscreen).toHaveBeenCalledOnce()
    // The detached component does not respond to later document events.
    expect(state.isFullscreen.value).toBe(true)
  })

  it('does not exit another component fullscreen on unmount', () => {
    const state = fixture()
    state.change({})
    lifecycle.unmount.forEach(callback => callback())
    expect(state.doc.exitFullscreen).not.toHaveBeenCalled()
  })
})
