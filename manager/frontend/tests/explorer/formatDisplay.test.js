import { describe, expect, it } from 'vitest'
import { dataFormatDisplayName } from '../../src/utils/formatDisplay'

describe('dataFormatDisplayName', () => {
  it('formats canonical 3D Tiles id for display', () => {
    expect(dataFormatDisplayName('3dtiles')).toBe('3D Tiles')
  })

  it('keeps common 3D and point-cloud formats readable', () => {
    expect(dataFormatDisplayName('glb')).toBe('GLB')
    expect(dataFormatDisplayName('las')).toBe('LAS')
  })

  it('formats SuperMap UDBX as a product format label', () => {
    expect(dataFormatDisplayName('udbx')).toBe('SuperMap UDBX')
  })

  it('uses the product name for Personal Geodatabase', () => {
    expect(dataFormatDisplayName('pgeo')).toBe('Personal Geodatabase')
  })

  it('distinguishes a generic Access database from Personal Geodatabase', () => {
    expect(dataFormatDisplayName('access')).toBe('Microsoft Access Database')
  })
})
