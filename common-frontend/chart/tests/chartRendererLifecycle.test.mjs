import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/ChartRenderer.vue', import.meta.url), 'utf8')

test('guards ECharts work until the container is measurable', () => {
  assert.match(source, /target\.clientWidth <= 0 \|\| target\.clientHeight <= 0/)
  assert.match(source, /if \(!hasRenderableSize\(element\.value\)\) return/)
})

test('does not retain a disposed ECharts instance across unmount callbacks', () => {
  assert.match(source, /chart\?\.dispose\(\)\s+chart = null/)
})
