import { describe, expect, it } from 'vitest'
import {
  lazPerfWasmAssetURL,
  locateLazPerfFile
} from '../../src/utils/pointCloudCOPCLazPerf'

describe('pointCloudCOPCLazPerf', () => {
  it('routes laz-perf wasm requests to the Vite-managed asset URL', () => {
    expect(locateLazPerfFile('laz-perf.wasm')).toBe(lazPerfWasmAssetURL)
    expect(locateLazPerfFile('/nested/laz-perf.wasm')).toBe(lazPerfWasmAssetURL)
  })

  it('leaves non-wasm runtime file paths unchanged', () => {
    expect(locateLazPerfFile('laz-perf.js')).toBe('laz-perf.js')
    expect(locateLazPerfFile('')).toBe('')
  })
})
