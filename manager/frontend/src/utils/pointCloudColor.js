import { Float32BufferAttribute } from 'three'
import { Lut } from 'three/examples/jsm/math/Lut.js'
import COPCSource from '@giro3d/giro3d/sources/COPCSource.js'

export const POINT_CLOUD_COLOR_MODES = Object.freeze(['rgb8', 'rgb16', 'elevation'])
export const elevationColors = new Lut('cooltowarm', 256).lut

export function pointCloudColorMode(value) {
  return POINT_CLOUD_COLOR_MODES.includes(value) ? value : 'rgb16'
}

export function normalizedPointColor(value, mode) {
  return Number.isFinite(value) ? Math.max(0, Math.min(1, value / (mode === 'rgb8' ? 255 : 65535))) : 0
}

export function elevationColor(z, min, max) {
  const ratio = max > min ? Math.max(0, Math.min(1, (z - min) / (max - min))) : 0.5
  return elevationColors[Math.round(ratio * (elevationColors.length - 1))]
}

// Manager's COPC display adapter keeps the source's 16-bit values intact.
// Each node uses the same explicitly selected full scale; never infer it per block.
export class ColorDisplayCOPCSource extends COPCSource {
  initialization = null
  initialized = false

  constructor(options, readMode) {
    super({ ...options, compressColorsTo8Bit: false })
    this.readMode = readMode
  }

  get ready() {
    return this.initialized
  }

  initialize() {
    // Giro3D 1.0.1 forks an unobserved .then() from its initialization promise.
    // Keep readiness and notification on the promise returned to the caller so
    // both cancellation and real read failures have exactly one rejection owner.
    this.initialization ??= this.initializeOnce().then(result => {
      this.initialized = true
      this.dispatchEvent({ type: 'initialized' })
      return result
    })
    return this.initialization
  }

  async getNodeData(params) {
    const mode = this.readMode()
    const data = await super.getNodeData(params)
    return {
      ...data,
      attributes: data.attributes.map((attribute, index) => {
        if (!attribute || params.attributes?.[index]?.interpretation !== 'color') return attribute
        const colors = Float32Array.from(attribute.array, value => normalizedPointColor(value, mode))
        return new Float32BufferAttribute(colors, 3)
      })
    }
  }
}
