import { test } from 'node:test'
import assert from 'node:assert/strict'
import { tiffPages, tiffPreviewSize, readTIFFRGBA } from '../src/utils/tiffPreview.js'

test('TIFF page directory requires complete, bounded, ordered facts', () => {
 const page = { ifd_index: 0, width: 2, height: 3 }
 const summary = { page_summary_status: 'parsed', page_count: 1, pages: [page] }
 assert.deepEqual(tiffPages(summary), [page])
 for (const status of ['invalid', 'unsupported', 'budget_exceeded', undefined]) {
  assert.deepEqual(tiffPages({ ...summary, page_summary_status: status }), [])
 }
 for (const pages of [[page, page], [{ ...page, ifd_index: 256 }], [{ ...page, width: 0 }], [{ ...page, ifd_index: -1 }]]) {
  assert.deepEqual(tiffPages({ ...summary, page_count: pages.length, pages }), [])
 }
 assert.deepEqual(tiffPages({ ...summary, page_count: 2 }), [])
 assert.deepEqual(tiffPreviewSize(2000, 1000), { width: 1024, height: 512 })
 assert.equal(tiffPreviewSize(16000001, 1), null)
 assert.equal(tiffPreviewSize(0, 3), null)
})

function image(fd, values) {
 return { fileDirectory: fd, readRasters: async () => values, readRGB: async () => values }
}

test('RGB16 is scaled and unassociated alpha zero survives', async () => {
 const rgba = await readTIFFRGBA(image({ PhotometricInterpretation: 2, BitsPerSample: [16, 16, 16, 16], ExtraSamples: [2] },
  [65535, 32768, 0, 0, 0, 0, 65535, 65535]), 2, 1)
 assert.deepEqual([...rgba], [255, 128, 0, 0, 0, 0, 255, 255])
})

test('associated alpha is unpremultiplied and grayscale preserves white', async () => {
 assert.deepEqual([...await readTIFFRGBA(image({ PhotometricInterpretation: 2, BitsPerSample: [8, 8, 8, 8], ExtraSamples: [1] }, [64, 0, 0, 128]), 1, 1)], [128, 0, 0, 128])
 assert.deepEqual([...await readTIFFRGBA(image({ PhotometricInterpretation: 1, BitsPerSample: [8] }, [0, 255]), 2, 1)], [0, 0, 0, 255, 255, 255, 255, 255])
 assert.deepEqual([...await readTIFFRGBA(image({ PhotometricInterpretation: 0, BitsPerSample: [1] }, [0, 1]), 2, 1)], [255, 255, 255, 255, 0, 0, 0, 255])
})

test('unsupported floating point samples are refused before decoding', async () => {
 await assert.rejects(readTIFFRGBA(image({ PhotometricInterpretation: 2, BitsPerSample: [32, 32, 32], SampleFormat: [3, 3, 3] }, []), 1, 1), /unsupportedSamples/)
})

test('palette conversion uses the decoder RGB8 output without grayscale remapping', async () => {
 const source = image({ PhotometricInterpretation: 3, BitsPerSample: [8] }, [255, 0, 0, 0, 255, 0])
 source.readRasters = () => { throw new Error('palette must use color map conversion') }
 assert.deepEqual([...await readTIFFRGBA(source, 2, 1)], [255, 0, 0, 255, 0, 255, 0, 255])
})
