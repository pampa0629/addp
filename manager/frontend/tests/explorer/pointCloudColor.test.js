import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BufferAttribute } from 'three'
const { readNode, optionsSeen } = vi.hoisted(() => ({ readNode: vi.fn(), optionsSeen: [] }))
vi.mock('@giro3d/giro3d/sources/COPCSource.js', () => ({ default: class {
  constructor(options) { optionsSeen.push(options) }
  getNodeData(params) { return readNode(params) }
} }))
import { ColorDisplayCOPCSource, elevationColor, normalizedPointColor, pointCloudColorMode } from '../../src/utils/pointCloudColor.js'

beforeEach(() => { readNode.mockReset(); optionsSeen.length = 0 })

describe('explicit point cloud color interpretation', () => {
  it('keeps 16-bit reading and preserves positions, non-color attributes and source bytes', async () => {
    const raw = new Uint16Array([1, 128, 255, 256, 32768, 65535])
    const original = raw.slice()
    const attribute = new BufferAttribute(raw, 3, true)
    const position = new BufferAttribute(new Float32Array([1, 2, 3, 4, 5, 6]), 3)
    const intensity = new BufferAttribute(new Uint16Array([1, 42]), 1)
    readNode.mockResolvedValue({ pointCount: 2, position, attributes: [intensity, attribute] })
    const source = new ColorDisplayCOPCSource({ url: 'existing.copc.laz' }, () => 'rgb8')
    const result = await source.getNodeData({ attributes: [{ interpretation: 'unknown' }, { interpretation: 'color' }] })
    expect(optionsSeen[0].compressColorsTo8Bit).toBe(false)
    expect(result.position).toBe(position)
    expect(result.attributes[0]).toBe(intensity)
    expect(result.attributes[1].array).toBeInstanceOf(Float32Array)
    expect(result.attributes[1].getX(0)).toBeCloseTo(1 / 255, 8)
    expect(result.attributes[1].getY(0)).toBeCloseTo(128 / 255, 6)
    expect(result.attributes[1].getZ(0)).toBe(1)
    expect(raw).toEqual(original)
  })

  it('does not stretch dark 16-bit data just because every value is below 256', async () => {
    readNode.mockResolvedValue({ attributes: [new BufferAttribute(new Uint16Array([1, 128, 255]), 3, true)] })
    let mode = 'rgb16'
    const source = new ColorDisplayCOPCSource({}, () => mode)
    const params = { attributes: [{ interpretation: 'color' }] }
    const result = await source.getNodeData(params)
    expect(result.attributes[0].getX(0)).toBeGreaterThan(0)
    expect(result.attributes[0].getZ(0)).toBeCloseTo(255 / 65535, 8)
    mode = 'rgb8'
    expect((await source.getNodeData(params)).attributes[0].getZ(0)).toBe(1)
    mode = 'rgb16'
    expect((await source.getNodeData(params)).attributes[0].getZ(0)).toBeCloseTo(255 / 65535, 8)
  })

  it('normalizes full 16-bit colors and clamps only to the chosen display range', () => {
    expect(normalizedPointColor(65535, 'rgb16')).toBe(1)
    expect(normalizedPointColor(32768, 'rgb16')).toBeCloseTo(0.5, 4)
    expect(normalizedPointColor(65535, 'rgb8')).toBe(1)
    expect(normalizedPointColor(-1, 'rgb8')).toBe(0)
    expect(normalizedPointColor(NaN, 'rgb8')).toBe(0)
    expect(pointCloudColorMode('guess')).toBe('rgb16')
    expect(pointCloudColorMode('elevation')).toBe('elevation')
  })

  it('maps the actual small height range and keeps planar/single-point data finite', () => {
    expect(elevationColor(0.001, 0.001, 0.002).equals(elevationColor(0.002, 0.001, 0.002))).toBe(false)
    expect(elevationColor(0, 0, 0).toArray().every(Number.isFinite)).toBe(true)
    expect(elevationColor(42, 42, 42)).toEqual(elevationColor(0.5, 0, 1))
  })
})
