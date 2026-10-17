import test from 'node:test'
import assert from 'node:assert/strict'
import { canDeclareSourceCRS, resolvePreviewSourceCRSDeclaration, TEMPORARY_SOURCE_CRS_OPTIONS } from '../src/utils/crsDeclaration.mjs'

test('unknown source coordinates have no default CRS and require a supported explicit declaration', () => {
  assert.equal(resolvePreviewSourceCRSDeclaration('', 'unknown_crs'), '')
  assert.equal(canDeclareSourceCRS('', 'unknown_crs'), true)
  for (const choice of TEMPORARY_SOURCE_CRS_OPTIONS) assert.equal(resolvePreviewSourceCRSDeclaration('', 'unknown_crs', choice), choice)
  for (const choice of ['', null, 'EPSG:4547', 'epsg:4326', '+proj=longlat']) assert.equal(resolvePreviewSourceCRSDeclaration('', 'unknown_crs', choice), '')
})

test('a stale CRS cannot override an explicit unknown status, and clearing restores suppression', () => {
  assert.equal(resolvePreviewSourceCRSDeclaration('EPSG:4326', 'unknown_crs'), '')
  assert.equal(resolvePreviewSourceCRSDeclaration('EPSG:4326', 'unknown_crs', 'EPSG:3857'), 'EPSG:3857')
  assert.equal(resolvePreviewSourceCRSDeclaration('EPSG:4326', 'unknown_crs', ''), '')
})

test('known or already transformed geometry never accepts a source declaration', () => {
  for (const source of ['EPSG:4326', 'EPSG:3857', 'EPSG:4547']) {
    assert.equal(canDeclareSourceCRS(source, 'not_transformed'), false)
    assert.equal(resolvePreviewSourceCRSDeclaration(source, 'not_transformed', 'EPSG:3857'), source)
    assert.equal(resolvePreviewSourceCRSDeclaration(source, 'engine_transformed', 'EPSG:3857'), source)
  }
  assert.equal(canDeclareSourceCRS('', 'engine_transformed'), false)
  assert.equal(resolvePreviewSourceCRSDeclaration('', 'engine_transformed', 'EPSG:3857'), '')
})
