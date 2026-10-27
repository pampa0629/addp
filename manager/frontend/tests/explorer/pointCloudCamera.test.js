import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as THREE from 'three'

vi.mock('vue', async importOriginal => ({
  ...await importOriginal(),
  watch: vi.fn(),
  onBeforeUnmount: vi.fn(),
  useSSRContext: () => ({ modules: new Set() })
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', () => ({ getAccessToken: () => '' }))
vi.mock('@giro3d/giro3d/sources/las/config.js', () => ({ setLazPerfPath: vi.fn() }))
vi.mock('@giro3d/giro3d/sources/COPCSource.js', () => ({ default: class {} }))
vi.mock('@giro3d/giro3d/entities/PointCloud.js', async () => {
  const { default: ColorMap } = await import('@giro3d/giro3d/core/ColorMap.js')
  const { Box3, Vector3, Color } = await import('three')
  return { default: class {
    ready = true
    colorMap = new ColorMap({ colors: [new Color('black'), new Color('white')], min: 0, max: 1000 })
    getBoundingBox() {
      return new Box3(new Vector3(-0.094689, 0.040011, -0.061873), new Vector3(0.061009, 0.187321, 0.058799))
    }
    getActiveAttributes() {
      return [{ attribute: { name: 'Z', interpretation: 'unknown', min: -0.061873, max: 0.058799 } }]
    }
    getAttributeColorMap() { return this.colorMap }
  } }
})
vi.mock('@giro3d/giro3d/core/Instance.js', async () => {
  const { default: View } = await import('@giro3d/giro3d/renderer/View.js')
  const { EventDispatcher, Scene } = await import('three')
  return { default: class extends EventDispatcher {
    constructor({ camera }) {
      super()
      // Use the real Giro3D View: its constructor resets the camera planes,
      // and its near setter enforces minNearPlane even with automatic fitting off.
      this.view = new View({ camera, width: 588, height: 539, crs: 'EPSG:3857' })
      this.scene = new Scene()
      this.renderer = { setPixelRatio() {}, setClearColor() {} }
      this.mainLoop = {}
      this.renderingOptions = {}
      this.domElement = new EventTarget()
    }
    notifyChange() {}
    async add() {}
  } }
})
vi.mock('three/examples/jsm/controls/OrbitControls.js', async () => {
  const { EventDispatcher, Vector3 } = await import('three')
  return { OrbitControls: class extends EventDispatcher {
    constructor(camera) {
      super()
      this.camera = camera
      this.target = new Vector3()
    }
    update() {
      this.camera.lookAt(this.target)
      this.camera.updateMatrixWorld()
    }
  } }
})

let PointCloudPreview
beforeEach(async () => {
  vi.stubGlobal('onmessage', null)
  vi.stubGlobal('window', {
    location: { origin: 'http://localhost', href: 'http://localhost/' },
    devicePixelRatio: 1,
    setInterval: vi.fn(() => 1),
    cancelAnimationFrame: vi.fn(),
    requestAnimationFrame: vi.fn(() => 1)
  })
  PointCloudPreview = (await import('../../src/components/explorer/PointCloudPreview.vue')).default
})
afterEach(() => vi.unstubAllGlobals())

const bunnyBounds = () => new THREE.Box3(
  new THREE.Vector3(-0.094689, 0.040011, -0.061873),
  new THREE.Vector3(0.061009, 0.187321, 0.058799)
)

function preview(viewState = {}) {
  const emit = vi.fn()
  const state = PointCloudPreview.setup({ data: {}, viewState }, { expose() {}, emit })
  state.viewportRef.value = {}
  const instance = state.ensureGiroInstance()
  return { state, view: instance.view, emit }
}

function expectBoundsVisible(view, bounds) {
  view.update()
  expect(view.isBox3Visible(bounds)).toBe(true)
  for (const x of [bounds.min.x, bounds.max.x]) {
    for (const y of [bounds.min.y, bounds.max.y]) {
      for (const z of [bounds.min.z, bounds.max.z]) {
        const point = new THREE.Vector3(x, y, z).project(view.camera)
        expect(point.z).toBeGreaterThan(-1)
        expect(point.z).toBeLessThan(1)
      }
    }
  }
}

describe('COPC camera clipping', () => {
  const offscreenBunnyView = {
    position: [-0.1290937428803298, 0.14804490511304996, 0.09989538879486387],
    target: [0.011023821091263708, 0.05090279841455564, 0.08494665594904667]
  }

  it.each([0.525, 1.3, 1.6])('refits the existing offscreen E57 view at aspect %s without regenerating', async aspect => {
    const { state, view, emit } = preview(offscreenBunnyView)
    view.setSize(539 * aspect, 539)
    await state.loadGiroCOPC('/existing-copc-content')
    const bounds = bunnyBounds()
    expect(state.giroControls.value.target.toArray()).toEqual(bounds.getCenter(new THREE.Vector3()).toArray())
    view.update()
    for (const x of [bounds.min.x, bounds.max.x]) {
      for (const y of [bounds.min.y, bounds.max.y]) {
        for (const z of [bounds.min.z, bounds.max.z]) {
          const point = new THREE.Vector3(x, y, z).project(view.camera)
          expect(Math.abs(point.x)).toBeLessThan(1)
          expect(Math.abs(point.y)).toBeLessThan(1)
        }
      }
    }
    expect(emit).toHaveBeenCalledWith('view-state-change', expect.objectContaining({
      target: bounds.getCenter(new THREE.Vector3()).toArray()
    }))
  })

  it('preserves a valid zoomed view instead of requiring the whole cloud to fit', () => {
    const { state, view, emit } = preview()
    const bounds = bunnyBounds()
    const target = bounds.getCenter(new THREE.Vector3())
    const position = target.clone().add(new THREE.Vector3(0.04, -0.06, 0.04))
    expect(state.applyGiroCameraViewState({ position: position.toArray(), target: target.toArray() }, bounds)).toBe(true)
    expect(view.camera.position.toArray()).toEqual(position.toArray())
    expect(emit).not.toHaveBeenCalled()
  })

  it('rejects a distant saved target relative to a small cloud even when looking through its center', () => {
    const { state } = preview()
    const center = bunnyBounds().getCenter(new THREE.Vector3())
    expect(state.applyGiroCameraViewState({
      position: center.clone().add(new THREE.Vector3(0, -0.5, 0)).toArray(),
      target: center.clone().add(new THREE.Vector3(0, 0.5, 0)).toArray()
    }, bunnyBounds())).toBe(false)
  })

  it('keeps the small E57 bunny inside the frustum after initial camera fitting', () => {
    const { state, view } = preview()
    const bounds = bunnyBounds()
    state.fitGiroCamera(bounds)
    expectBoundsVisible(view, bounds)
  })

  it('keeps the small cloud visible when restoring a saved camera position', () => {
    const { state, view } = preview()
    const bounds = bunnyBounds()
    expect(state.applyGiroCameraViewState({
      position: [0.88316, -1.186334, 0.898463],
      target: [-0.01684, 0.113666, -0.001537]
    }, bounds)).toBe(true)
    expectBoundsVisible(view, bounds)
  })

  it('continues fitting a large cloud with a distant world-space origin', () => {
    const { state, view } = preview()
    const bounds = bunnyBounds()
    bounds.min.multiplyScalar(1e6)
    bounds.max.multiplyScalar(1e6)
    bounds.translate(new THREE.Vector3(13000000, 4000000, 500))
    state.fitGiroCamera(bounds)
    expect(view.far).toBeGreaterThan(view.camera.position.distanceTo(bounds.getCenter(new THREE.Vector3())))
    expectBoundsVisible(view, bounds)
  })

  it.each([0.525, 1.6])('frames a small cloud at its real scale in viewport aspect %s', aspect => {
    const { state, view } = preview()
    view.setSize(539 * aspect, 539)
    state.fitGiroCamera(bunnyBounds())
    view.update()
    const projected = []
    const bounds = bunnyBounds()
    for (const x of [bounds.min.x, bounds.max.x]) {
      for (const y of [bounds.min.y, bounds.max.y]) {
        for (const z of [bounds.min.z, bounds.max.z]) {
          const point = new THREE.Vector3(x, y, z).project(view.camera)
          expect(Math.abs(point.x)).toBeLessThan(1)
          expect(Math.abs(point.y)).toBeLessThan(1)
          projected.push(point)
        }
      }
    }
    const width = Math.max(...projected.map(p => p.x)) - Math.min(...projected.map(p => p.x))
    const height = Math.max(...projected.map(p => p.y)) - Math.min(...projected.map(p => p.y))
    expect(Math.max(width, height)).toBeGreaterThan(1)
  })

  it('uses the actual Z range so a loaded small cloud is not uniformly black', async () => {
    const { state } = preview()
    await state.loadGiroCOPC('/copc-content')
    const colorMap = state.giroPointCloud.value.getAttributeColorMap('Z')
    expect(colorMap.sample(-0.061873).r).toBe(0)
    expect(colorMap.sample(0.058799).r).toBe(1)
  })
})
