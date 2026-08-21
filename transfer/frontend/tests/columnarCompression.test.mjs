import test from 'node:test'
import assert from 'node:assert/strict'

import {
  normalizeColumnarCompressionCapability,
  resolveColumnarCompression,
  withColumnarCompressionOption
} from '../src/views/TaskWizard/columnarCompression.mjs'

const capability = normalizeColumnarCompressionCapability({
  codecs: ['zstd', 'snappy', 'lz4_raw', 'brotli', 'gzip', 'uncompressed'],
  default: 'zstd'
})

test('normalizes the backend capability and resolves its default', () => {
  assert.deepEqual(capability, {
    codecs: ['zstd', 'snappy', 'lz4_raw', 'brotli', 'gzip', 'uncompressed'],
    default: 'zstd'
  })
  assert.equal(resolveColumnarCompression(capability), 'zstd')
})

test('rejects malformed capabilities and undeclared selections', () => {
  assert.equal(normalizeColumnarCompressionCapability({ codecs: ['zstd'], default: 'snappy' }), null)
  assert.equal(resolveColumnarCompression(capability, 'none'), '')
})

test('adds the selected codec without mutating format options', () => {
  const base = { max_rows_per_row_group: 1000 }
  assert.deepEqual(withColumnarCompressionOption(base, capability, 'snappy'), {
    max_rows_per_row_group: 1000,
    compression: 'snappy'
  })
  assert.deepEqual(base, { max_rows_per_row_group: 1000 })
})

test('leaves non-columnar format options unchanged', () => {
  assert.deepEqual(withColumnarCompressionOption({ header: true }, null), { header: true })
})
