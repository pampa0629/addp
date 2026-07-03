import createLazPerf from 'laz-perf/lib/web/laz-perf.js'
import lazPerfWasmURL from 'laz-perf/lib/web/laz-perf.wasm?url'

export const lazPerfWasmAssetURL = lazPerfWasmURL

export function locateLazPerfFile(path) {
  return String(path || '').endsWith('laz-perf.wasm') ? lazPerfWasmURL : path
}

export function createCOPCLazPerf() {
  return createLazPerf({
    locateFile: locateLazPerfFile
  })
}
