import { describe, expect, it } from 'vitest'
import { maxTextureDeclarations } from '../../src/utils/maxTextureDeclarations.js'

describe('MAX explicit texture declarations', () => {
  it('preserves exact embedded references and accepts an empty declaration', () => {
    expect(maxTextureDeclarations([])).toEqual({ mapping: {} })
    expect(maxTextureDeclarations([{ reference: 'C:\\old\\brick.png', path: 'textures/brick.png' }])).toEqual({ mapping: { 'C:\\old\\brick.png': 'textures/brick.png' } })
  })
  it('rejects incomplete and duplicate declarations', () => {
    expect(maxTextureDeclarations([{ reference: 'brick', path: '' }]).error).toBe('incomplete')
    expect(maxTextureDeclarations([{ reference: 'brick', path: 'a.png' }, { reference: 'brick', path: 'b.png' }]).error).toBe('duplicate')
  })
  it.each(['/a.png', '../a.png', 'sub/../a.png', 'C:/a.png', 'sub\\a.png', '\u0000.png', '.', './'])('rejects out-of-directory image %s', path => {
    expect(maxTextureDeclarations([{ reference: 'brick', path }]).error).toBe('outsideDirectory')
  })
})
