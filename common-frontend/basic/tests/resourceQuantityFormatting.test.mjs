import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { formatBytes, scaleByteValue, formatDurationSeconds } from '../src/utils/formatters.js'

test('byte quantities use IEC units, preserve zero, and reject invalid evidence', () => {
  assert.equal(formatBytes(67304611840, 2, 'zh-CN'), '62.68 GiB')
  assert.equal(formatBytes(1024 ** 2, 2, 'en'), '1.00 MiB')
  assert.equal(formatBytes(0, 2), '0.00 B')
  assert.deepEqual(scaleByteValue(1024), { value: 1, unit: 'KiB', divisor: 1024 })
  for (const value of [null, undefined, NaN, Infinity, -1, 'bad']) assert.equal(formatBytes(value), '-')
  assert.equal(formatBytes('1024', 2), '1.00 KiB')
})

test('elapsed seconds use localized duration components, not a date or time zone', () => {
  assert.equal(formatDurationSeconds(286435, 'zh-CN'), '3天 7小时 33分钟')
  assert.equal(formatDurationSeconds(286435, 'en'), '3 days 7 hours 33 minutes')
  assert.equal(formatDurationSeconds(0, 'zh-CN'), '0秒钟')
  assert.equal(formatDurationSeconds(59.9, 'en'), '59 seconds')
  assert.equal(formatDurationSeconds(60, 'zh-CN'), '1分钟')
  assert.equal(formatDurationSeconds(3600, 'en'), '1 hour')
  assert.equal(formatDurationSeconds(86400, 'zh-CN'), '1天')
  for (const value of [null, undefined, NaN, Infinity, -1, '60']) assert.equal(formatDurationSeconds(value), '-')
})

test('map and file size entry points use the sole shared byte formatter', () => {
  const map = readFileSync(new URL('../../map/src/utils/formatters.js', import.meta.url), 'utf8')
  assert.match(map, /export \{ formatBytes \} from .*basic\/src\/utils\/formatters.js/)
  assert.doesNotMatch(map, /function formatBytes/)
  const utils = readFileSync(new URL('../src/utils/index.js', import.meta.url), 'utf8')
  assert.match(utils, /return formatBytes\(bytes, 2\)/)
  assert.doesNotMatch(utils, /Math.log\(bytes\)/)
})
