import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { buildStaticTrajectory, buildTrajectorySelection, trajectoryTime, validateTrajectoryScope, validateTrajectoryTable } from '../src/utils/staticTrajectory.mjs'
const config = { device_field: 'unit', time_field: 'at', x_field: 'east', y_field: 'north', coordinate_mode: 'columns', time_encoding: 'iso' }
const rows = [
  { unit: 'A', at: '2026-10-16T02:00:00Z', east: '117', north: '32' },
  { unit: 'B', at: '2026-10-16T01:00:00Z', east: 118, north: 33 },
  { unit: 'A', at: '2026-10-16T09:00:00+08:00', east: 116, north: 31 }
]
const table = (values = rows) => ({ columns: ['unit', 'at', 'east', 'north', 'shape'], rows: values, total: values.length, page: 1, has_more: false })
const build = (data, options = config, transform = value => value) => buildStaticTrajectory(data, options, transform)
const rejects = (callback, code, row) => assert.throws(callback, error => error.code === code && (row == null || error.row === row))

test('groups nonadjacent devices, sorts instants across timezones, and leaves source immutable', () => {
  const data = table(), before = structuredClone(data)
  const result = build(data)
  assert.deepEqual(result.tracks[0].coordinates, [[116, 31], [117, 32]])
  assert.equal(result.tracks[0].start, '2026-10-16T01:00:00.000Z')
  assert.deepEqual(result.features.map(f => f.geometry.type), ['LineString', 'Point'])
  assert.equal(result.pointCount, 3)
  assert.deepEqual(data, before)
  assert.deepEqual(Object.keys(result.features[0].properties), ['device', 'points', 'start', 'end'])
})
test('accepts complete unknown totals, but refuses incomplete, missing facts, capped and truncated pages', () => {
  assert.equal(validateTrajectoryTable({ ...table(), total: null }).rows.length, 3)
  for (const change of [{ page: 2 }, { has_more: true }, { has_more: undefined }, { total: 100 }, { total: 2 }, { object: { content: { truncated: true } } }]) rejects(() => build({ ...table(), ...change }), 'incomplete')
  rejects(() => build(table(Array.from({ length: 2001 }, () => rows[0]))), 'limit')
  rejects(() => build({ rows: rows }), 'not_table')
  rejects(() => build(table([])), 'empty')
})
test('uses explicit fields and refuses partial mappings or invisible columns', () => {
  rejects(() => build(table(), { ...config, device_field: '' }), 'configuration')
  rejects(() => build(table(), { ...config, x_field: 'geom' }), 'configuration')
  rejects(() => build(table(), { ...config, x_field: 'north' }), 'configuration')
  rejects(() => build({ ...table(), columns: ['at', 'east', 'north'] }), 'configuration')
})
test('rejects invalid devices, coordinates, unzoned times, and conflicting locations without partial output', () => {
  for (const unit of [null, '', {}, true, 9007199254740992]) rejects(() => build(table([{ ...rows[0], unit }])), 'device', 1)
  for (const east of [null, '', false, 'Infinity', '0x10', '116 masked', 181]) rejects(() => build(table([{ ...rows[0], east }])), 'coordinate', 1)
  rejects(() => build(table([{ ...rows[0], at: '2026-10-16T02:00:00' }])), 'time', 1)
  rejects(() => build(table([rows[0], { ...rows[0], east: 118 }])), 'conflict', 2)
  assert.equal(build(table([rows[0], rows[0]])).pointCount, 2)
  assert.equal(build(table([rows[0], rows[0]])).features[0].geometry.type, 'Point')
})
test('time encodings are explicit, calendar-valid and millisecond-precise', () => {
  assert.equal(trajectoryTime('1.25', 'unix_s'), 1250)
  assert.equal(trajectoryTime('1.001', 'unix_s'), 1001)
  assert.equal(trajectoryTime('-0.001', 'unix_s'), -1)
  assert.equal(trajectoryTime('1760580000.001', 'unix_s'), 1760580000001)
  assert.equal(trajectoryTime('1250', 'unix_ms'), 1250)
  assert.equal(trajectoryTime('2024-02-29T12:00:00.123Z', 'iso'), Date.parse('2024-02-29T12:00:00.123Z'))
  for (const value of ['2026-02-29T00:00:00Z', '2026-04-31T00:00:00Z', '2026-10-16T24:00:00Z', '2026-10-16T01:00:00.1234Z', '', 0]) rejects(() => trajectoryTime(value, 'iso'), 'time')
  for (const value of ['', null, true, '1e3', '1.001']) rejects(() => trajectoryTime(value, 'unix_ms'), 'time')
})
test('accepts explicitly selected Point geometries and applies the shared CRS transformation', () => {
  const values = rows.map(row => ({ ...row, shape: JSON.stringify({ type: 'Point', coordinates: [row.east, row.north] }) }))
  const options = { ...config, coordinate_mode: 'point', geometry_field: 'shape' }
  assert.equal(build(table(values), options).tracks.length, 2)
  const projected = build(table([{ ...rows[0], east: 0, north: 0 }]), config, geometry => ({ ...geometry, coordinates: [1, 2] }))
  assert.deepEqual(projected.tracks[0].coordinates, [[1, 2]])
  rejects(() => build(table([{ ...rows[0], shape: { type: 'LineString', coordinates: [[1, 2], [3, 4]] } }]), options), 'point')
  rejects(() => build(table([{ ...rows[0], shape: { type: 'Point', coordinates: [1, 2, 3] } }]), options), 'point')
  rejects(() => build(table(), config, () => null), 'coordinate')
  rejects(() => build(table([{ ...rows[0], shape: { type: 'Feature', geometry: { type: 'Point' } } }]), options), 'point', 1)
})
test('trajectory rendering has one shared owner and Manager only adapts its controlled preview', () => {
  const owner = readFileSync(new URL('../../../manager/frontend/src/components/explorer/PreviewPanel.vue', import.meta.url).pathname, 'utf8')
  assert.match(owner, /<StaticTrajectoryPreview\b/)
  assert.doesNotMatch(owner, /buildStaticTrajectory|new MapContainer|LineString/)
})

test('builds explicit typed device/time selections and refuses ambiguous ranges', () => {
  const range = { device_kind: 'string', devices: 'A\nB', start: '2026-10-16T01:00:00Z', end: '2026-10-16T03:00:00Z' }
  const selection = buildTrajectorySelection(config, range)
  assert.deepEqual(selection.devices, ['A', 'B'])
  assert.deepEqual(buildTrajectorySelection(config, { ...range, device_kind: 'integer', devices: '1\n2' }).devices, [1, 2])
  for (const change of [{ devices: '' }, { devices: 'A\nA' }, { end: range.start }, { start: '2026-10-16T01:00:00' }, { device_kind: 'integer', devices: '9007199254740992' }, { device_kind: 'integer', devices: '1.2' }]) rejects(() => buildTrajectorySelection(config, { ...range, ...change }), 'range')
})
test('accepts only an exact complete server selection instead of an ignored filter or a stale response', () => {
  const selection = buildTrajectorySelection(config, { device_kind: 'string', devices: 'A', start: '2026-10-16T01:00:00Z', end: '2026-10-16T03:00:00Z' })
  const data = { ...table(), trajectory_scope: { selection, complete: true, scanned_rows: 3003 } }
  assert.equal(validateTrajectoryScope(data, selection), data)
  for (const scope of [undefined, { ...data.trajectory_scope, complete: false }, { ...data.trajectory_scope, scanned_rows: 1 }, { ...data.trajectory_scope, selection: { ...selection, devices: ['B'] } }, { ...data.trajectory_scope, selection: { ...selection, start: 'other' } }]) rejects(() => validateTrajectoryScope({ ...data, trajectory_scope: scope }, selection), 'scope')
})
