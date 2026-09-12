import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'

const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8')

test('Meta exposes scan tasks while Monitor owns the module-wide execution list', () => {
  const router = read('../src/router/index.js')
  const layout = read('../src/components/Layout.vue')
  const scan = read('../src/views/MetadataScan.vue')

  assert.doesNotMatch(router, /TaskMonitor|path:\s*['"]tasks['"]/)
  assert.doesNotMatch(layout, /index=['"]\/tasks['"]/)
  assert.match(scan, /MonitorExecutionsButton/)
  assert.match(scan, /module="meta"/)
  assert.match(scan, /task-type="scan"/)
})
